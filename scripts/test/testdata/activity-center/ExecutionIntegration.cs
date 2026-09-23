using System.Collections;
using System.Globalization;
using System.IO;
using System.Net;
using System.Net.Http;
using System.Reflection;
using System.Resources;
using System.Text;
using System.Text.Json;
using System.Text.Json.Nodes;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Input;
using System.Windows.Markup;
using System.Windows.Media;
using System.Windows.Threading;
using System.Xml.Linq;
using AgentDock.ControlPanel;

internal static partial class Program
{
    private sealed partial class LocalFixture
    {
        internal bool Integration115 { get; set; }
        internal string InsertionStatus { get; set; } = "pending";
        internal int InsertionPosts { get; private set; }
        internal DateTimeOffset ExecutionNow { get; } = DateTimeOffset.UtcNow;
        private object SidebarConversation(string id, string title)
        {
            var value = JsonSerializer.SerializeToNode(Conversation(id, title), ActivityClient.JsonOptions)!.AsObject();
            if (Integration115)
            {
                value["statistics"]!["last_tool_call_at"] = ExecutionNow;
                value["statistics"]!["last_activity_at"] = ExecutionNow;
            }
            return value;
        }
        private async Task<bool> RespondExecution115Async(HttpListenerContext context)
        {
            var path = context.Request.Url!.AbsolutePath;
            if (path == "/internal/runtime/execution/sidebar")
            {
                using var body = await JsonDocument.ParseAsync(context.Request.InputStream);
                var query = body.RootElement; var search = query.Text("search");
                var limit = (int)query.Field("limits").Number("wsp_fixture");
                object[] all = ExecutionScaleMode
                    ? Enumerable.Range(0, ScaleObjectCount).Where(index => $"对话 {index + 1:D4}".Contains(search, StringComparison.Ordinal)).Select(ScaleConversationObject).ToArray()
                    : new[] { SidebarConversation(ConversationA, "执行中心升级 · 当前对话"), SidebarConversation(ConversationB, "独立并行对话 · 不串线") };
                var take = limit > 0 ? limit : search.Length > 0 || query.Text("view") != "active" ? 200 : 5;
                var page = all.Take(take).ToArray();
                var selected = all.FirstOrDefault(item => JsonSerializer.SerializeToElement(item, ActivityClient.JsonOptions).Text("conversation_id") == query.Text("selected_id"));
                await JsonAsync(context, new { server_now = ExecutionNow, groups = new[] { new
                {
                    workspace_id = "wsp_fixture", title = "사용자 프로젝트 中文", root = _root, total = all.Length,
                    shown = page.Length, recent_count = all.Length, has_more = page.Length < all.Length,
                    last_activity_at = ExecutionNow, conversations = page
                } }, selected });
                return true;
            }
            if (path.EndsWith("/insertions", StringComparison.Ordinal))
            {
                if (context.Request.HttpMethod == "POST")
                {
                    using var body = await JsonDocument.ParseAsync(context.Request.InputStream);
                    Require(body.RootElement.EnumerateObject().Select(item => item.Name).Order().SequenceEqual(new[] { "submission_id", "text" }), "Composer sent forged ownership arguments.");
                    InsertionPosts++;
                }
                await JsonAsync(context, new { insertions = Integration115 ? new[] { new { insertion_id = "ins_fixture", status = InsertionStatus } } : Array.Empty<object>() });
                return true;
            }
            return false;
        }
    }

    private static void AwaitExecution(Task task)
    {
        PumpUntil(() => task.IsCompleted, TimeSpan.FromSeconds(10));
        task.GetAwaiter().GetResult();
    }
    private static void TestIntegration115(string root)
    {
        SynchronizationContext.SetSynchronizationContext(new DispatcherSynchronizationContext(Dispatcher.CurrentDispatcher));
        var app = new Application { ShutdownMode = ShutdownMode.OnExplicitShutdown };
        using (var styles = typeof(Program).Assembly.GetManifestResourceStream("ActualAppStyles.xaml")!)
        {
            var source = XDocument.Load(styles).Root!;
            XNamespace presentation = "http://schemas.microsoft.com/winfx/2006/xaml/presentation";
            var resources = source.Element(presentation + "Application.Resources")!;
            var dictionary = resources.Element(presentation + "ResourceDictionary") ?? new XElement(presentation + "ResourceDictionary", source.Attributes().Where(attribute => attribute.IsNamespaceDeclaration), resources.Elements());
            app.Resources = (ResourceDictionary)XamlReader.Parse(dictionary.ToString(), new ParserContext { BaseUri = new Uri("pack://application:,,,/agentdock-tray;component/") });
        }
        var manager = new ResourceManager("AgentDock.ControlPanel.Resources.UiStrings", typeof(UiText).Assembly);
        var english = manager.GetResourceSet(CultureInfo.InvariantCulture, true, false)!.Cast<DictionaryEntry>().ToDictionary(item => (string)item.Key, item => (string)item.Value!);
        foreach (var locale in new[] { "ko-KR", "en", "zh-CN" })
        {
            ApplyTestUiLanguage(locale);
            var resourcesForLocale = locale == "en" ? english : manager.GetResourceSet(CultureInfo.GetCultureInfo(locale), true, false)!
                .Cast<DictionaryEntry>().ToDictionary(item => (string)item.Key, item => (string)item.Value!);
            Require(resourcesForLocale.Count == english.Count, "Satellite resource count differs: " + locale);
            foreach (var (key, value) in english)
            {
                var localized = resourcesForLocale.GetValueOrDefault(key);
                Require(!string.IsNullOrWhiteSpace(localized), "Missing localized key " + locale + "/" + key);
                // JSON examples are literal text rather than composite-format strings.
                if (System.Text.RegularExpressions.Regex.IsMatch(value, @"\{[0-9]+[^{}]*\}"))
                    Require(CompositeFormat.Parse(localized!).MinimumArgumentCount == CompositeFormat.Parse(value).MinimumArgumentCount, "Changed format arguments " + key);
            }
            var directory = Path.Combine(root, locale); Directory.CreateDirectory(directory);
            var preferencesPath = Path.Combine(directory, "execution-center-settings.json");
            File.WriteAllText(preferencesPath, "{\"schema_version\":2,\"font_size\":16,\"future_field\":{\"revision\":1}}");
            using var fixture = new LocalFixture(directory) { ExecutionMode = true, Integration115 = true }; fixture.WriteRuntime(directory);
            using var runtime = new RuntimeService(directory);
            var window = new ExecutionWindow(runtime) { ShowActivated = false, ShowInTaskbar = false, Left = -12000, Top = -12000, WindowStartupLocation = WindowStartupLocation.Manual };
            var trace = new BindingErrors(); System.Diagnostics.PresentationTraceSources.DataBindingSource.Listeners.Add(trace);
            window.Show();
            try
            {
                PumpUntil(() => window.Objects.Count(item => !item.IsGroupFooter) == 2 && window.Calls.Count == 2 && ((Button)window.FindName("InsertButton")).IsEnabled, TimeSpan.FromSeconds(12));
                ((DispatcherTimer)typeof(ExecutionWindow).GetField("_pulse", BindingFlags.NonPublic | BindingFlags.Instance)!.GetValue(window)!).Stop();
                var objects = (ListBox)window.FindName("ObjectsList"); var first = window.Objects.First(item => !item.IsGroupFooter);
                var before = window.Objects.ToArray();
                AwaitExecution((Task)InvokeExecution(window, "LoadSidebarAsync")!);
                Require(window.Objects.Zip(before).All(pair => ReferenceEquals(pair.First, pair.Second)), "Sidebar polling replaced stable row identities.");
                Require(first.WorkspaceKey.Title == "사용자 프로젝트 中文" && first.Title == "执行中心升级 · 当前对话", "User-provided titles were translated.");
                var group = new WorkspaceGroupKey("missing", ""); group.Apply(JsonSerializer.SerializeToElement(new { title = "历史工作区", title_source = "historical_workspace" }));
                Require(group.Title == UiText.Get("ExecutionHistoricalWorkspace"), "Owned project fallback was not localized by provenance.");
                var input = (TextBox)window.FindName("InsertionTextBox");
                Require(((Button)window.FindName("SendInsertionButton")).Content?.ToString() == UiText.Get("ExecutionSendInsertion"), "Loaded insertion action missed locale.");
                Require(input.ToolTip?.ToString() == UiText.Get("ExecutionInsertionHelp") && input.AcceptsReturn, "Composer lost localized expiry/read disclaimer or multiline input.");
                input.Text = "한글 IME\nkeep user text";
                var ime = typeof(ExecutionWindow).GetField("_imeComposing", BindingFlags.NonPublic | BindingFlags.Instance)!;
                ime.SetValue(window, true);
                var key = new KeyEventArgs(Keyboard.PrimaryDevice, PresentationSource.FromVisual(input)!, Environment.TickCount, Key.Return) { RoutedEvent = Keyboard.PreviewKeyDownEvent };
                InvokeExecution(window, "Insertion_KeyDown", input, key);
                Require(!key.Handled && fixture.InsertionPosts == 0, "IME confirmation sent the draft."); ime.SetValue(window, false);
                objects.SelectedItem = window.Objects.Single(item => item.Id == LocalFixture.ConversationB);
                PumpUntil(() => window.Calls.Count == 0, TimeSpan.FromSeconds(5));
                Require(input.Text == "", "Draft crossed conversation ownership.");
                objects.SelectedItem = first;
                PumpUntil(() => window.Calls.Count == 2 && input.Text == "한글 IME\nkeep user text", TimeSpan.FromSeconds(5));
                input.Text = new string('한', 2731);
                Require(!((Button)window.FindName("SendInsertionButton")).IsEnabled && ((TextBlock)window.FindName("InsertionStatus")).Text == UiText.Get("ExecutionInsertionTextLimit"), "UTF-8 byte limit was not enforced.");
                input.Clear();
                foreach (var state in new[] { "pending", "reserved", "attached", "expired", "cancelled", "delivery_unknown", "target_changed" })
                {
                    fixture.InsertionStatus = state; AwaitExecution((Task)InvokeExecution(window, "ReadInsertionsAsync", first.Id)!);
                    var resource = state switch { "reserved" => "Reserved", "attached" => "Attached", "expired" => "Expired", "cancelled" => "Cancelled", "delivery_unknown" => "DeliveryUnknown", "target_changed" => "TargetChanged", _ => "Pending" };
                    var expected = state == "pending" ? UiText.Format("ExecutionInsertionPending", 1) : UiText.Get("ExecutionInsertion" + resource);
                    Require(((TextBlock)window.FindName("InsertionStatus")).Text == expected, "Insertion state mistranslated: " + state);
                }
                // Keep only retention notices dismissible across sessions.
                InvokeExecution(window, "Warn", "retention", "activity_retention_gap"); InvokeExecution(window, "DismissWarning_Click", window, new RoutedEventArgs());
                InvokeExecution(window, "Warn", "storage diagnostic"); InvokeExecution(window, "Warn", "retention replay", "activity_retention_gap");
                Require(((TextBlock)window.FindName("WarningText")).Text == "storage diagnostic", "Dismissal hid an unrelated storage error.");
                var disk = JsonNode.Parse(File.ReadAllText(preferencesPath))!.AsObject(); disk["future_field"]!["revision"] = 2; File.WriteAllText(preferencesPath, disk.ToJsonString());
                InvokeExecution(window, "SavePreferences");
                var saved = JsonNode.Parse(File.ReadAllText(preferencesPath))!;
                Require(saved["schema_version"]!.GetValue<int>() == 3 && saved["font_size"]!.GetValue<double>() == 16 && saved["future_field"]!["revision"]!.GetValue<int>() == 2, "Schema migration lost existing or newly written unknown preferences.");
                foreach (var refused in new[] { "{broken", "null", "{\"schema_version\":4,\"theme\":\"dark\"}" })
                {
                    File.WriteAllText(preferencesPath, refused);
                    InvokeExecution(window, "SavePreferences");
                    Require(File.ReadAllText(preferencesPath) == refused, "Execution settings overwrote corrupt/future preferences.");
                    var rejected = false; try { DesktopTheme.Save("light"); } catch (JsonException) { rejected = true; }
                    Require(rejected && File.ReadAllText(preferencesPath) == refused, "Theme settings overwrote corrupt/future preferences.");
                }
                File.WriteAllText(preferencesPath, saved.ToJsonString());
                TestIntegrationMcpAndNotification(runtime, window, locale);
                Require(fixture.InsertionPosts == 0 && fixture.ControlCount == 0 && fixture.LastBatchIDs.Length == 0 && fixture.UnauthorizedRequests == 0, "Display regression mutated live task state or bypassed authentication.");
                Require(trace.Messages.Count == 0, "Changed-view binding failures: " + string.Join("\n", trace.Messages));
            }
            finally { window.Close(); System.Diagnostics.PresentationTraceSources.DataBindingSource.Listeners.Remove(trace); }
            PumpUntil(() => fixture.ActiveStreams == 0, TimeSpan.FromSeconds(5));
            Console.WriteLine("Loaded integration controls and schema-3 preservation passed: " + locale);
        }
        DesktopTheme.Dispose();
    }

    private static void TestIntegrationMcpAndNotification(RuntimeService runtime, ExecutionWindow execution, string locale)
    {
        var main = new MainWindow(runtime);
        try
        {
            var build = typeof(MainWindow).GetMethod("BuildMcpCapabilityRow", BindingFlags.Instance | BindingFlags.NonPublic)!;
            foreach (var known in new[] { false, true })
            {
                var server = new McpCapabilityInfo { Name = "user_mcp", Status = "ready", ToolCountKnown = known, ToolCount = 7, ServerVersion = "2.5.1", Description = "user description" };
                var row = (Border)build.Invoke(main, new object[] { server, false, "" })!;
                var host = new Window { Content = row, ShowActivated = false, ShowInTaskbar = false, Left = -12000, Top = -12000, Width = 480, Height = 140 };
                host.Show();
                try
                {
                    var expected = known ? UiText.Format("McpStatusSummary", "ready", 7) : UiText.Format("McpStatusUnknownCount", "ready");
                    Require(Descendants(row).OfType<TextBlock>().Any(text => text.Text.Contains(expected) && text.Text.Contains("2.5.1")), "MCP discovered metadata or unknown count was misrepresented.");
                    foreach (var theme in new[] { "dark", "light" })
                    {
                        DesktopTheme.Save(theme); host.UpdateLayout();
                        foreach (var text in Descendants(row).OfType<ThemeTextBlock>())
                            Require(text.Foreground is SolidColorBrush actual && text.FindResource(text.Text == "user_mcp" ? "PrimaryText" : "SecondaryText") is SolidColorBrush wanted && actual.Color == wanted.Color, "MCP text did not follow shared dynamic theme.");
                    }
                }
                finally { host.Content = null; host.Close(); }
            }
        }
        finally { main.Close(); }
        var notification = new TaskNotification("completion_fixture", LocalFixture.TaskId, "사용자 완료 제목 中文", LocalFixture.ConversationB, LocalFixture.BranchId);
        TaskNotification? routed = null;
        var popup = new TaskNotificationWindow(notification, item => { routed = item; return Task.CompletedTask; }) { Left = -12000, Top = -12000 };
        popup.Show(); popup.UpdateLayout();
        try
        {
            Require(Descendants(popup).OfType<TextBlock>().Any(text => text.Text == UiText.Get("ExecutionTaskCompletedNotification")), "Loaded completion notice missed " + locale);
            var button = Descendants(popup).OfType<Button>().Single(value => value.Content is TextBlock);
            ClickDialogButton(button);
            Require(ReferenceEquals(routed, notification) && routed.ConversationId == LocalFixture.ConversationB && routed.ThreadId == LocalFixture.BranchId, "Completion notification retargeted the latest selected conversation.");
        }
        finally { if (popup.IsVisible) popup.Close(); }
        using var response = new HttpResponseMessage(HttpStatusCode.Conflict);
        var error = (Exception)typeof(ActivityClient).GetMethod("ResponseError", BindingFlags.Static | BindingFlags.NonPublic)!.Invoke(null, new object[] { response, Encoding.UTF8.GetBytes("{\"error\":{\"code\":\"INSERTION_CONFLICT\",\"message\":\"ORIGINAL diagnostic 中文\"}}") })!;
        Require(error.Message.Contains(UiText.Get("ExecutionApi_INSERTION_CONFLICT")) && error.Message.Contains("ORIGINAL diagnostic 中文"), "Stable-code API guidance lost locale or original diagnostics.");
    }
}
