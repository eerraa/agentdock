using System.Diagnostics;
using System.Globalization;
using System.IO;
using System.Reflection;
using System.Text.Json;
using System.Windows;
using System.Windows.Automation;
using System.Windows.Controls;
using System.Windows.Media;
using System.Windows.Media.Imaging;
using System.Windows.Threading;
using AgentDock.ControlPanel;

internal static partial class Program
{
    private static void TestLocalizedExecution(string root)
    {
        Directory.CreateDirectory(root);
        var originalPreference = ((CultureInfo)typeof(UiText).GetField("_resourceCulture", BindingFlags.NonPublic | BindingFlags.Static)!.GetValue(null)!).Name;
        var metrics = new List<object>();
        try
        {
            foreach (var language in new[] { "ko-KR", "en", "zh-CN" })
            {
                ApplyTestUiLanguage(language);
                var directory = Path.Combine(root, language);
                Directory.CreateDirectory(directory);
                using var fixture = new LocalFixture(directory) { ExecutionMode = true };
                fixture.WriteRuntime(directory);
                using var runtime = new RuntimeService(directory);
                var window = new ExecutionWindow(runtime) { ShowActivated = false, ShowInTaskbar = false, WindowStartupLocation = WindowStartupLocation.Manual, Left = -12000, Top = -12000 };
                var trace = new BindingErrors(); PresentationTraceSources.DataBindingSource.Listeners.Add(trace);
                window.Show();
                try
                {
                    PumpUntil(() => window.Objects.Count(item => !item.IsGroupFooter) == 2 && window.Calls.Count == 2 && window.Calls.Any(call => call.UpdatedSeq == 4), TimeSpan.FromSeconds(12));
                    ((DispatcherTimer)typeof(ExecutionWindow).GetField("_pulse", BindingFlags.NonPublic | BindingFlags.Instance)!.GetValue(window)!).Stop();
                    window.UpdateLayout();
                    Require(Descendants(window).OfType<Button>().Any(button => button.Content?.ToString() == UiText.Get("ExecutionPermissions")), "Loaded execution view missed its selected locale.");
                    Require(window.Objects[0].Title == "执行中心升级 · 当前对话", "A supplied conversation title was translated or replaced.");
                    Require(window.Calls.Single(call => call.Id == LocalFixture.PendingCall).State == UiText.Get("ExecutionPendingApproval"), "Loaded pending-approval state was not localized.");
                    InvokeExecution(window, "WarnTerminatedConversation");
                    Require(((TextBlock)window.FindName("WarningText")).Text == UiText.Get("ExecutionConversationTerminated"), "Termination warning missed the selected locale.");
                    Require((string)typeof(ExecutionWindow).GetField("_warningCode", BindingFlags.NonPublic | BindingFlags.Instance)!.GetValue(window)! == "conversation-terminated", "Warning state depends on translated text.");
                    InvokeExecution(window, "ShowConnectionInfo", "fixture connection");
                    Require((string)typeof(ExecutionWindow).GetField("_infoDetailsCode", BindingFlags.NonPublic | BindingFlags.Instance)!.GetValue(window)! == "connection", "Connection detail did not retain its stable presentation code.");
                    InvokeExecution(window, "ShowInfo", UiText.Get("ExecutionConnection"), "different detail with same visible title");
                    Require((string)typeof(ExecutionWindow).GetField("_infoDetailsCode", BindingFlags.NonPublic | BindingFlags.Instance)!.GetValue(window)! == "", "A coincidentally identical title reused the connection update target.");
                    InvokeExecution(window, "CloseDetails"); InvokeExecution(window, "Warn", "");
                    foreach (var theme in new[] { "light", "dark", "system" })
                    {
                        InvokeExecution(window, "ApplyTheme", theme);
                        foreach (var scale in language == "ko-KR" ? new[] { 1.0, 1.25, 1.5, 2.0 } : new[] { 1.0 })
                            CaptureExecution(window, Path.Combine(directory, $"loaded-{theme}-{scale*100:0}.png"), 840, 640, scale);
                    }
                    var originalFontSize = window.FontSize;
                    foreach (var fontSize in new[] { 12d, 14d, 20d })
                    {
                        window.FontSize = fontSize;
                        window.UpdateLayout();
                        var statusFilter = (ComboBox)window.FindName("CallStatusCombo");
                        var presentation = (RadioButton)window.FindName("CompactCallsChoice");
                        var statusBounds = statusFilter.TransformToAncestor(window).TransformBounds(new Rect(statusFilter.RenderSize));
                        var presentationBounds = presentation.TransformToAncestor(window).TransformBounds(new Rect(presentation.RenderSize));
                        Require(!statusBounds.IntersectsWith(presentationBounds), $"Execution filter overlaps the presentation controls at minimum window size ({language}, {fontSize} DIP): filter={statusBounds}; presentation={presentationBounds}.");
                        var bar = (FrameworkElement)((FrameworkElement)statusFilter.Parent).Parent;
                        var barBounds = bar.TransformToAncestor(window).TransformBounds(new Rect(bar.RenderSize));
                        Require(barBounds.Contains(statusBounds) && barBounds.Contains(presentationBounds), $"Wrapped controls are clipped outside their allocated toolbar row ({language}, {fontSize} DIP): toolbar={barBounds}; filter={statusBounds}; presentation={presentationBounds}.");
                        CaptureExecution(window, Path.Combine(directory, $"minimum-font-{fontSize:0}.png"), 840, 640, 1.0);
                    }
                    window.FontSize = originalFontSize;
                    window.UpdateLayout();
                    InvokeExecution(window, "SidebarMenu_Click", window.FindName("ObjectsList"), new RoutedEventArgs());
                    var menu = ((ListBox)window.FindName("ObjectsList")).ContextMenu;
                    Require(menu is { IsOpen: true } && menu.Items.OfType<MenuItem>().Any(item => item.Header?.ToString() == UiText.Get("ExecutionCurrentConversation")), "The opened management menu missed the selected locale.");
                    PumpUntil(() => menu!.ActualWidth > 0 && menu.ActualHeight > 0, TimeSpan.FromSeconds(3)); menu!.UpdateLayout(); CaptureLocalizedElement(menu, Path.Combine(directory, "management-menu.png")); menu.IsOpen = false;
                    TestLocalizedExecutionDialogs(window, directory);
                    Require(trace.Messages.Count == 0, "Localized execution binding failures: " + string.Join("\n", trace.Messages));
                    metrics.Add(new { language, loaded_conversations = window.Objects.Count, loaded_calls = window.Calls.Count, menu_opened = true, dialog_actions_verified = true, scope = "isolated HTTP, real WPF windows, synthetic raster scaling; not actual OS DPI or installed-package acceptance" });
                }
                finally { window.Close(); PresentationTraceSources.DataBindingSource.Listeners.Remove(trace); }
                PumpUntil(() => fixture.ActiveStreams == 0, TimeSpan.FromSeconds(6));
                Require(fixture.ControlCount == 0 && fixture.LastBatchIDs.Length == 0, "Localization, navigation, modal fixtures or closing the observer dispatched a task mutation.");
            }
        }
        finally { ApplyTestUiLanguage(originalPreference); }
        File.WriteAllText(Path.Combine(root, "localized-execution-result.json"), JsonSerializer.Serialize(metrics, new JsonSerializerOptions { WriteIndented = true }));
    }

    private static void TestLocalizedExecutionDialogs(ExecutionWindow owner, string root)
    {
        var cancelled = RunLocalizedModal(owner, () => ExecutionDialogs.Confirm(owner, UiText.Get("ExecutionPermanentlyDelete"), UiText.Format("ExecutionDeleteRecordsWarning", 17)), dialog =>
        {
            var accept = DialogControl<Button>(dialog, "ExecutionConfirmAccept");
            var cancel = DialogControl<Button>(dialog, "ExecutionConfirmCancel");
            Require(accept.Content?.ToString() == UiText.Get("ExecutionConfirm") && !accept.IsDefault && cancel.IsCancel, "Localized confirmation changed its safe default or caption.");
            CaptureLocalizedElement((FrameworkElement)dialog.Content, Path.Combine(root, "delete-confirmation.png"));
            ClickDialogButton(cancel);
        });
        Require(cancelled is false, "Cancelling confirmation accepted the operation.");
        const string preservedInput = "사용자 지정 中文 user title";
        var prompt = RunLocalizedModal(owner, () => ExecutionDialogs.Prompt(owner, UiText.Get("ExecutionRename"), UiText.Get("ExecutionEnterName"), preservedInput), dialog =>
        {
            Require(DialogControl<TextBox>(dialog, "ExecutionPromptValue").Text == preservedInput, "Localized prompt rewrote user input.");
            ClickDialogButton(DialogControl<Button>(dialog, "ExecutionPromptConfirm"));
        });
        Require(prompt is string value && value == preservedInput, "The prompt did not preserve its submitted value.");
        var chosen = RunLocalizedModal(owner, () => ExecutionDialogs.Choose(owner, UiText.Get("ExecutionLinkExistingTask"), UiText.Get("ExecutionLinkWithoutSwitching"), [new("tsk_fixture", preservedInput)]), dialog =>
        {
            Descendants(dialog).OfType<TextBox>().Single().Text = "tsk_explicit-unchanged";
            ClickDialogButton(Descendants(dialog).OfType<Button>().Single(button => button.Content?.ToString() == UiText.Get("ExecutionLinkTask")));
        });
        Require(chosen is string id && id == "tsk_explicit-unchanged", "Link selection translated a task identifier.");

        var longPath = @"D:\한글 작업 공간\" + string.Concat(Enumerable.Repeat("매우 긴 경로 부분 ", 60)) + "target.txt";
        var fixedRequest = JsonSerializer.Serialize(new { action = "patch", path = longPath, original_text = preservedInput });
        var approval = JsonSerializer.SerializeToElement(new
        {
            approval = new { status = "pending", conversation_id = "conv_fixture", task_id = "", tool = "file_edit", rule_id = "test-rule", reason = "fixture reason", scope_description = longPath, workspace_id = "wsp_fixture" },
            request_available = true, fixed_request = fixedRequest, rule_preview = new { effect = "allow", tool = "file_edit", workspace_id = "wsp_fixture" }
        });
        var approved = RunLocalizedModal(owner, () => ExecutionDialogs.Approve(owner, approval), dialog =>
        {
            Require(dialog.Title == UiText.Get("ExecutionApprovalTitle"), "Approval title missed the selected locale.");
            var request = DialogControl<TextBox>(dialog, "ApprovalFixedRequest");
            Require(request.Text == fixedRequest && request.IsReadOnly, "Approval changed the immutable request or made it editable.");
            CaptureLocalizedElement((FrameworkElement)dialog.Content, Path.Combine(root, "long-path-approval.png"));
            Require(request.ActualHeight >= 80, $"Long scope hid the fixed approval request: actual height={request.ActualHeight:0.##} DIP.");
            Descendants(dialog).OfType<CheckBox>().Single().IsChecked = true;
            var allow = DialogControl<Button>(dialog, "ApprovalApproveOnce");
            Require(allow.Content?.ToString() == UiText.Get("ExecutionApproveSaveRule"), "Rule-grant approval text did not update dynamically.");
            ClickDialogButton(allow);
        });
        Require(approved is ValueTuple<string, bool> accepted && accepted.Item1 == "approve" && accepted.Item2, "Localized approval changed its action or scope grant.");
        var rejected = RunLocalizedModal(owner, () => ExecutionDialogs.Approve(owner, approval), dialog => ClickDialogButton(DialogControl<Button>(dialog, "ApprovalReject")));
        Require(rejected is ValueTuple<string, bool> denied && denied.Item1 == "reject" && !denied.Item2, "Localized rejection changed its action or granted permission.");
        var unavailable = JsonSerializer.SerializeToElement(new { approval = new { status = "expired", workspace_id = "wsp_fixture" }, request_available = false });
        var expired = RunLocalizedModal(owner, () => ExecutionDialogs.Approve(owner, unavailable), dialog =>
        {
            Require(!DialogControl<Button>(dialog, "ApprovalApproveOnce").IsEnabled && !DialogControl<Button>(dialog, "ApprovalReject").IsEnabled, "Expired approval became actionable.");
            ClickDialogButton(Descendants(dialog).OfType<Button>().Single(button => button.Content?.ToString() == UiText.Get("ExecutionBack")));
        });
        Require(expired is null, "Closing expired approval submitted a decision.");
        var policy = JsonSerializer.SerializeToElement(new { policy = new { revision = 9, global_mode = "rules", rules = Array.Empty<object>(), scopes = Array.Empty<object>() }, effective = new { mode = "rules", scope = "global" }, workspaces = Array.Empty<object>() });
        var changed = RunLocalizedModal(owner, () => ExecutionDialogs.Permissions(owner, policy), dialog =>
        {
            var modes = DialogControl<ComboBox>(dialog, "PermissionMode");
            modes.SelectedItem = modes.Items.Cast<ExecutionChoice>().Single(item => item.Id == "readonly");
            CaptureLocalizedElement((FrameworkElement)dialog.Content, Path.Combine(root, "permission-policy.png"));
            ClickDialogButton(DialogControl<Button>(dialog, "PermissionSave"));
        });
        Require(changed is Dictionary<string, object> change && (string)change["mode"] == "readonly" && (string)change["scope"] == "global" && (long)change["expected_revision"] == 9, "Localization changed permission scope, mode or revision ownership.");
    }

    // Exercise Button.OnClick including its IsCancel/command behavior, not just a routed event.
    private static void ClickDialogButton(Button button) => typeof(Button).GetMethod("OnClick", BindingFlags.NonPublic | BindingFlags.Instance)!.Invoke(button, null);

    private static T DialogControl<T>(Window dialog, string id) where T : FrameworkElement => Descendants(dialog).OfType<T>().Single(item => AutomationProperties.GetAutomationId(item) == id);
    private static object? RunLocalizedModal(Window owner, Func<object?> show, Action<Window> inspect)
    {
        Exception? failure = null; var inspected = false;
        var timer = new DispatcherTimer(DispatcherPriority.ApplicationIdle) { Interval = TimeSpan.FromMilliseconds(100) };
        timer.Tick += (_, _) =>
        {
            var dialog = owner.OwnedWindows.Cast<Window>().LastOrDefault(window => window.IsVisible);
            if (dialog is null) return;
            timer.Stop(); inspected = true;
            try { dialog.UpdateLayout(); inspect(dialog); }
            catch (Exception error) { failure = error; }
            finally { if (dialog.IsVisible) dialog.Close(); }
        };
        timer.Start(); object? result;
        try { result = show(); }
        finally { timer.Stop(); }
        if (failure is not null) throw new InvalidOperationException("Localized modal regression failed.", failure);
        Require(inspected, "The actual modal window was not inspected.");
        return result;
    }
    private static void CaptureLocalizedElement(FrameworkElement element, string path)
    {
        element.UpdateLayout();
        Require(element.ActualWidth > 0 && element.ActualHeight > 0, "Localized UI fixture was not laid out.");
        var image = new RenderTargetBitmap((int)Math.Ceiling(element.ActualWidth), (int)Math.Ceiling(element.ActualHeight), 96, 96, PixelFormats.Pbgra32);
        // Normalize the element's parent-relative margin; otherwise the bitmap
        // clips its last rows even though the live window has no such clipping.
        var visual = new DrawingVisual();
        using (var drawing = visual.RenderOpen())
            drawing.DrawRectangle(new VisualBrush(element) { Stretch = Stretch.Fill }, null, new Rect(0, 0, element.ActualWidth, element.ActualHeight));
        image.Render(visual); var encoder = new PngBitmapEncoder(); encoder.Frames.Add(BitmapFrame.Create(image));
        using var file = File.Create(path); encoder.Save(file);
    }
}
