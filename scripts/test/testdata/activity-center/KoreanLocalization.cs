using System.Collections;
using System.Globalization;
using System.IO;
using System.Reflection;
using System.Resources;
using System.Text;
using System.Text.RegularExpressions;
using System.Text.Json;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Media;
using System.Windows.Media.Imaging;
using AgentDock.ControlPanel;

internal static partial class Program
{
    private static void ApplyTestUiLanguage(string preference) => typeof(UiText)
        .GetMethod("ApplyPreference", BindingFlags.Static | BindingFlags.NonPublic)!
        .Invoke(null, new object[] { preference });

    private static void TestKoreanLocalization(string root)
    {
        Directory.CreateDirectory(root);
        const BindingFlags flags = BindingFlags.Static | BindingFlags.NonPublic;
        var apply = typeof(UiText).GetMethod("ApplyPreference", flags)!;
        var resourceCulture = typeof(UiText).GetField("_resourceCulture", flags)!;
        var previousResource = resourceCulture.GetValue(null);
        var previousUi = CultureInfo.CurrentUICulture;
        var previousDefault = CultureInfo.DefaultThreadCurrentUICulture;
        var previousFormat = CultureInfo.CurrentCulture;
        var preferencePath = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "AgentDock", "ui-language");
        var previousPreference = File.Exists(preferencePath) ? File.ReadAllBytes(preferencePath) : null;
        void Apply(string preference) => apply.Invoke(null, new object[] { preference });
        try
        {
            foreach (var culture in new[] { "ko", "ko-KR", "ko-KP", " KO-kr " })
                Require(UiText.NormalizeCultureName(culture) == "ko-KR", "Korean system culture was not normalized: " + culture);
            Require(UiText.ResolveLocale("system", "ko-KR") == "ko-KR", "System Korean preference was lost.");
            Require(UiText.NormalizePreference(" ko-KR ") == "ko-KR", "Persisted Korean preference was rejected.");
            Require(UiText.ResolveLocale("ko-KR", "en-US") == "ko-KR", "Explicit Korean did not override system English.");
            Require(UiText.ResolveLocale("en", "ko-KR") == "en", "Explicit English did not override system Korean.");
            Require(UiText.ResolveLocale("zh-CN", "ko-KR") == "zh-CN", "Explicit Chinese did not override system Korean.");
            Require(UiText.ResolveLocale("invalid", "ja-JP") == "en", "Unsupported language fallback changed.");
            Require(UiText.NormalizeCultureName("zh-Hans-SG") == "zh-CN", "Existing Chinese culture mapping regressed.");

            var manager = new ResourceManager("AgentDock.ControlPanel.Resources.UiStrings", typeof(UiText).Assembly);
            var english = manager.GetResourceSet(CultureInfo.InvariantCulture, true, false)!;
            var korean = manager.GetResourceSet(CultureInfo.GetCultureInfo("ko-KR"), true, false);
            Require(korean is not null, "Korean satellite resources were not built.");
            var keyCount = 0;
            foreach (DictionaryEntry entry in english)
            {
                var key = (string)entry.Key;
                var source = (string)entry.Value!;
                var translated = korean!.GetString(key);
                Require(!string.IsNullOrWhiteSpace(translated), "Korean satellite is missing: " + key);
                if (Regex.IsMatch(source, @"\{[0-9]"))
                {
                    var sourceFormat = CompositeFormat.Parse(source);
                    var translatedFormat = CompositeFormat.Parse(translated!);
                    Require(sourceFormat.MinimumArgumentCount == translatedFormat.MinimumArgumentCount, "Composite format changed: " + key);
                }
                keyCount++;
            }
            Require(keyCount >= 340, "Unexpectedly incomplete localization fixture.");
            Require(manager.GetString("MainWindowTitle", CultureInfo.GetCultureInfo("ja-JP")) == "AgentDock Control Panel", "ResourceManager fallback changed.");

            CultureInfo.CurrentCulture = CultureInfo.GetCultureInfo("en-US");
            Apply("ko-KR");
            Require(CultureInfo.CurrentCulture.Name == "en-US", "Language selection changed number/date formatting culture.");
            Require(UiText.Get("MainWindowTitle") == "AgentDock 제어판", "Selected Korean resource was not used.");
            var offlineUpdate = new UpdateCheckResult { Code = "online-updates-disabled", Message = "raw technical message" };
            Require(offlineUpdate.DisplayMessage == UiText.Get("OfflineUpdatesOnly") && offlineUpdate.DisplayMessage.Contains("수동 업데이트"), "Stable offline update code did not select Korean guidance.");
            var unknownUpdate = new UpdateCheckResult { Code = "future-code", Message = "unrecognized diagnostic" };
            Require(unknownUpdate.DisplayMessage == unknownUpdate.Message, "Unknown update diagnostics were discarded.");
            Require((string)new LocExtension("Overview").ProvideValue(null!) == "개요", "XAML localization did not use Korean.");
            Require(ActivityText.Get("Title") == "AgentDock · 작업 활동 센터", "Activity text ignored the selected Korean locale.");
            Require(ActivityText.State("cancelled") == "취소됨" && ActivityText.Kind("command.started") == "명령", "Activity state/kind was not localized.");
            Require(ActivityText.State("future-state") == "future-state" && ActivityText.Kind("future.event") == "future.event" && ActivityText.Get("missing") == "missing", "Activity fallback changed protocol values.");
            Require(UiText.Format("ActivityCancelConfirmation", 2).Contains("중지하지 않습니다"), "Task cancellation was confused with process stopping.");
            var asyncActivity = Task.Run(async () => { CultureInfo.CurrentUICulture = CultureInfo.GetCultureInfo("en-US"); await Task.Yield(); return ActivityText.Get("Title"); }).GetAwaiter().GetResult();
            Require(asyncActivity == "AgentDock · 작업 활동 센터", "Async activity rendering lost the explicit language.");
            Require(ExecutionJson.State("pending_approval") == "승인 대기" && ExecutionJson.State("unknown") == "결과 확인 필요", "Execution states changed meaning or missed Korean.");
            Require(ExecutionJson.State("future-status") == "future-status" && ExecutionJson.Mode("future-mode") == "future-mode", "Unknown execution protocol codes were translated.");
            var manualTitle = ExecutionObject.From(JsonSerializer.SerializeToElement(new { title = "新对话", title_source = "manual" }), "conversation");
            Require(manualTitle.Title == "新对话", "A user title matching a historical label was rewritten.");
            var defaultTitle = ExecutionObject.From(JsonSerializer.SerializeToElement(new { title = "legacy default", title_source = "fallback" }), "conversation");
            Require(defaultTitle.Title == "대화 · 이전 기록", "The stable default-title source did not select localized guidance.");
            var missingTiming = new ExecutionCallRow(JsonSerializer.SerializeToElement(new { call_id = "ko-fixture", tool_name = "file_edit", status = "unknown" }));
            Require(missingTiming.Duration == "기록 없음" && !missingTiming.CanRetry && missingTiming.Technical.Contains("file_edit"), "Missing timing, retry safety, or technical identity was changed.");
            var preview = new ExecutionCallRow(JsonSerializer.SerializeToElement(new { call_id = "ko-preview", file_edit = new { action = "patch", dry_run = true, executed = true, changed = false, path = @"D:\한글 경로\source.txt" } }));
            Require(preview.FileEditDetails.Contains("파일에 쓰지 않음") && preview.FileEditDetails.Contains("실제 변경됨: 아니요") && preview.FileEditDetails.Contains("영향을 받은 파일 수: 기록 없음"), "Korean preview guidance fabricated a write or result count.");
            Require(UiText.Get("MissingLocalizationTestKey") == "MissingLocalizationTestKey", "Missing-key fallback changed.");
            Require(UiText.Format("LastRefresh", new DateTime(2026, 9, 22, 13, 2, 3)).Contains("13:02:03"), "Date placeholder formatting changed.");
            var resumed = Task.Run(async () =>
            {
                CultureInfo.CurrentUICulture = CultureInfo.GetCultureInfo("en-US");
                await Task.Yield();
                return UiText.Get("MainWindowTitle");
            }).GetAwaiter().GetResult();
            Require(resumed == "AgentDock 제어판", "An async continuation reverted selected Korean resources.");
            Apply("en");
            Require(UiText.Get("MainWindowTitle") == "AgentDock Control Panel", "Switch back to English failed.");
            Apply("zh-CN");
            Require(UiText.Get("MainWindowTitle") == "AgentDock 控制面板", "Switch back to Chinese failed.");
            Apply("ko-KR");

            using var fixture = new LocalFixture(root);
            fixture.WriteRuntime(root);
            using var runtime = new RuntimeService(root);
            var window = new MainWindow(runtime);
            try
            {
                Require(window.Title == "AgentDock 제어판", "New control panel did not render the selected language.");
                var selector = (ComboBox)window.FindName("LanguageComboBox");
                var choices = selector.Items.OfType<ComboBoxItem>().ToArray();
                foreach (var tag in new[] { "system", "en", "zh-CN", "ko-KR" })
                    Require(choices.Count(choice => (string)choice.Tag == tag) == 1, "Language selection changed identity: " + tag);
                Require((string)choices.Single(choice => (string)choice.Tag == "ko-KR").Content == "한국어", "Korean choice label is missing.");
                var grid = (Grid)window.Content;
                var tabs = grid.Children.OfType<TabControl>().Single();
                foreach (var tabKey in new[] { "AdvancedSettings", "DisplaySettings" })
                {
                    tabs.SelectedItem = tabs.Items.OfType<TabItem>().Single(tab => (string)tab.Header == UiText.Get(tabKey));
                    foreach (var scale in new[] { 1.0, 1.5, 2.0 })
                        CaptureKoreanControlPanel(window, root, tabKey, scale);
                }
            }
            finally { window.CloseForReplacement(); }
            File.WriteAllText(Path.Combine(root, "localization-result.json"), System.Text.Json.JsonSerializer.Serialize(new
            {
                passed = true, resource_keys = keyCount, system_and_explicit_language = true,
                english_and_chinese_preserved = true, async_resource_culture = true,
                offscreen_tabs = new[] { "AdvancedSettings", "DisplaySettings" },
                offscreen_scales = new[] { 1.0, 1.5, 2.0 },
                production_preference_write = false,
                scope = "UiStrings and Display tab only; execution-center hardcoded strings and installer are not covered"
            }));
        }
        finally
        {
            resourceCulture.SetValue(null, previousResource);
            CultureInfo.CurrentUICulture = previousUi;
            CultureInfo.DefaultThreadCurrentUICulture = previousDefault;
            CultureInfo.CurrentCulture = previousFormat;
            var after = File.Exists(preferencePath) ? File.ReadAllBytes(preferencePath) : null;
            Require(previousPreference is null ? after is null : after is not null && previousPreference.SequenceEqual(after), "Localization tests modified the production language preference.");
        }
    }

    private static void CaptureKoreanControlPanel(MainWindow window, string root, string tab, double scale)
    {
        // Render the actual WPF content without showing a window or starting Core.
        var content = (FrameworkElement)window.Content;
        window.Content = null;
        var surface = new Border { Background = window.Background, Child = content };
        try
        {
            const double width = 860, height = 780;
            surface.Measure(new Size(width, height));
            surface.Arrange(new Rect(0, 0, width, height));
            surface.UpdateLayout();
            var names = tab == "AdvancedSettings" ? new[] { "LanguageComboBox", "PortTextBox" } : new[] { "ThemePreferenceCombo", "McpUiEnabledChoice" };
            foreach (var name in names)
            {
                var element = (FrameworkElement)window.FindName(name);
                var bounds = element.TransformToAncestor(surface).TransformBounds(new Rect(0, 0, element.ActualWidth, element.ActualHeight));
                Require(bounds.Width > 0 && bounds.Height > 0 && bounds.Left >= 0 && bounds.Top >= 0 && bounds.Right <= width + 1 && bounds.Bottom <= height + 1, "Korean control clipped: " + name);
            }
            var image = new RenderTargetBitmap((int)(width * scale), (int)(height * scale), 96 * scale, 96 * scale, PixelFormats.Pbgra32);
            image.Render(surface);
            var encoder = new PngBitmapEncoder();
            encoder.Frames.Add(BitmapFrame.Create(image));
            using var output = File.Create(Path.Combine(root, $"korean-{tab}-{scale.ToString(CultureInfo.InvariantCulture)}.png"));
            encoder.Save(output);
        }
        finally { surface.Child = null; window.Content = content; }
    }
}
