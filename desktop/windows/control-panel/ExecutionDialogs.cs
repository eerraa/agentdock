using System.Text.Json;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Media;
using System.Windows.Automation;
using Button = System.Windows.Controls.Button;
using CheckBox = System.Windows.Controls.CheckBox;
using ComboBox = System.Windows.Controls.ComboBox;
using MessageBox = System.Windows.MessageBox;
using TextBox = System.Windows.Controls.TextBox;
using Color = System.Windows.Media.Color;
using HorizontalAlignment = System.Windows.HorizontalAlignment;
using FontFamily = System.Windows.Media.FontFamily;
using Orientation = System.Windows.Controls.Orientation;

namespace AgentDock.ControlPanel;

internal static class ExecutionDialogs
{
    private static (Window Window, DockPanel Root, StackPanel Actions) Create(Window owner, string title, int width = 620, int height = 460)
    {
        var window = new Window { Owner = owner, Title = title, Width = width, Height = height, MinWidth = 360, MinHeight = 160, ShowInTaskbar = false, WindowStartupLocation = WindowStartupLocation.CenterOwner, FontFamily = owner.FontFamily, FontSize = owner.FontSize, Resources = owner.Resources, Background = owner.Background, Foreground = owner.Foreground };
        var root = new DockPanel { Margin = new Thickness(18) }; window.Content = root;
        var actions = new StackPanel { Orientation = Orientation.Horizontal, HorizontalAlignment = HorizontalAlignment.Right, Margin = new Thickness(0, 12, 0, 0) }; DockPanel.SetDock(actions, Dock.Bottom); root.Children.Add(actions);
        return (window, root, actions);
    }
    private static Button Action(string text, string? automationId = null)
    {
        var button = new Button { Content = text, Padding = new Thickness(16, 7, 16, 7), Margin = new Thickness(4, 0, 0, 0), MinHeight = 34 };
        if (automationId is not null) AutomationProperties.SetAutomationId(button, automationId);
        return button;
    }
    private static TextBlock Label(string value) => new() { Text = value, TextWrapping = TextWrapping.Wrap, Margin = new Thickness(0, 8, 0, 6) };
    private static TextBox Readonly(string value) => new() { Text = value, IsReadOnly = true, AcceptsReturn = true, TextWrapping = TextWrapping.Wrap, VerticalScrollBarVisibility = ScrollBarVisibility.Auto, Padding = new Thickness(10), FontFamily = new FontFamily("Consolas, Microsoft YaHei UI") };
    internal static void ShowText(Window owner, string title, string text)
    {
        var ui = Create(owner, title); var close = Action(ExecutionText.Get("DialogClose")); close.Click += (_, _) => ui.Window.Close(); ui.Actions.Children.Add(close); ui.Root.Children.Add(Readonly(text)); ui.Window.ShowDialog();
    }
    internal static bool Confirm(Window owner, string title, string explanation, string? confirm = null)
    {
        confirm ??= ExecutionText.Get("DialogConfirm");
        var ui = Create(owner, title, 500, 230);
        ui.Window.ResizeMode = ResizeMode.NoResize;
        ui.Root.Children.Add(Label(explanation));
        var cancel = Action(ExecutionText.Get("DialogCancel"), "ExecutionConfirmCancel"); cancel.IsCancel = true;
        var ok = Action(confirm, "ExecutionConfirmAccept");
        cancel.Click += (_, _) => ui.Window.Close();
        ok.Click += (_, _) => ui.Window.DialogResult = true;
        ui.Actions.Children.Add(cancel); ui.Actions.Children.Add(ok);
        ui.Window.Loaded += (_, _) => cancel.Focus();
        return ui.Window.ShowDialog() == true;
    }
    internal static string? Prompt(Window owner, string title, string explanation, string initial)
    {
        var ui = Create(owner, title, 500, 220); var panel = new StackPanel(); panel.Children.Add(Label(explanation));
        var box = new TextBox { Text = initial, TextWrapping = TextWrapping.Wrap, MinHeight = 42, MaxHeight = 150, Padding = new Thickness(8) }; AutomationProperties.SetAutomationId(box, "ExecutionPromptValue"); panel.Children.Add(box); ui.Root.Children.Add(panel);
        string? result = null; var ok = Action(ExecutionText.Get("DialogConfirm"), "ExecutionPromptConfirm"); var cancel = Action(ExecutionText.Get("DialogCancel"));
        ok.Click += (_, _) => { result = box.Text.Trim(); ui.Window.DialogResult = true; }; cancel.Click += (_, _) => ui.Window.Close(); ui.Actions.Children.Add(cancel); ui.Actions.Children.Add(ok);
        ui.Window.Loaded += (_, _) => { box.Focus(); box.SelectAll(); }; ui.Window.ShowDialog(); return result;
    }
    internal static string? Choose(Window owner, string title, string explanation, IReadOnlyList<ExecutionChoice> choices)
    {
        var ui = Create(owner, title, 560, 290); var panel = new StackPanel(); panel.Children.Add(Label(explanation));
        var combo = new ComboBox { ItemsSource = choices, DisplayMemberPath = "Title", MinHeight = 34, IsEditable = false }; panel.Children.Add(combo);
        panel.Children.Add(Label(ExecutionText.Get("OrEnterTaskId"))); var text = new TextBox { MinHeight = 34, Padding = new Thickness(6) }; panel.Children.Add(text); ui.Root.Children.Add(panel);
        string? result = null; var confirm = Action(ExecutionText.Get("LinkTask")); confirm.Click += (_, _) => { result = text.Text.Trim().Length > 0 ? text.Text.Trim() : (combo.SelectedItem as ExecutionChoice)?.Id; if (result is not null) ui.Window.DialogResult = true; }; ui.Actions.Children.Add(confirm); ui.Window.ShowDialog(); return result;
    }
    internal static (string Action, bool AllowWorkspace)? Approve(Window owner, JsonElement detail)
    {
        var a = detail.Field("approval"); var pending = a.Text("status") == "pending" && detail.Flag("request_available");
        var ui = Create(owner, ExecutionText.Get("ApprovalTitle"), 840, 730);
        var panel = new DockPanel(); ui.Root.Children.Add(panel);
        var header = Label(ExecutionText.Format("ApprovalHeader", a.Text("conversation_id"), a.Text("task_id") == "" ? ExecutionText.Get("NoLinkedTask") : a.Text("task_id"), a.Text("tool"), a.Text("rule_id"), a.Text("reason"), a.Text("scope_description"), pending ? ExecutionText.Get("NotYetExecuted") : a.Text("status"))); DockPanel.SetDock(header, Dock.Top); panel.Children.Add(header);
        var grant = new CheckBox { Content = ExecutionText.Get("AllowWorkspaceTools"), IsEnabled = pending && a.Text("workspace_id") != "", Margin = new Thickness(0, 9, 0, 5) };
        var grantPanel = new StackPanel(); grantPanel.Children.Add(grant); var rule = Readonly(detail.Field("rule_preview").Pretty()); rule.Height = 120; grantPanel.Children.Add(rule); DockPanel.SetDock(grantPanel, Dock.Bottom); panel.Children.Add(grantPanel);
        var fixedText = Readonly(detail.Text("fixed_request")); AutomationProperties.SetAutomationId(fixedText, "ApprovalFixedRequest"); panel.Children.Add(fixedText);
        (string, bool)? result = null; var reject = Action(ExecutionText.Get("RejectApproval"), "ApprovalReject"); var once = Action(ExecutionText.Get("AllowOnce"), "ApprovalApproveOnce"); var cancel = Action(ExecutionText.Get("Back"));
        reject.IsEnabled = once.IsEnabled = pending;
        grant.Checked += (_, _) => once.Content = ExecutionText.Get("ApproveAndSaveRule"); grant.Unchecked += (_, _) => once.Content = ExecutionText.Get("AllowOnce");
        reject.Click += (_, _) => { result = ("reject", false); ui.Window.DialogResult = true; };
        once.Click += (_, _) => { result = ("approve", grant.IsChecked == true); ui.Window.DialogResult = true; };
        cancel.Click += (_, _) => ui.Window.Close(); ui.Actions.Children.Add(cancel); ui.Actions.Children.Add(reject); ui.Actions.Children.Add(once); ui.Window.ShowDialog(); return result;
    }
    internal static object? Permissions(Window owner, JsonElement detail)
    {
        var ui = Create(owner, ExecutionText.Get("PermissionsTitle"), 840, 710); var policy = detail.Field("policy");
        var panel = new StackPanel(); ui.Root.Children.Add(new ScrollViewer { Content = panel, VerticalScrollBarVisibility = ScrollBarVisibility.Auto });
        panel.Children.Add(Label(ExecutionText.Format("PermissionSummary", ExecutionJson.Mode(detail.Field("effective").Text("mode")), ExecutionJson.Mode(policy.Text("global_mode")), policy.Number("revision"))));
        var scopes = new List<ExecutionChoice> { new("global:", ExecutionText.Get("GlobalDefault")) };
        var conversationId = detail.Text("conversation_id");
        if (conversationId.Length > 0) scopes.Add(new("conversation:" + conversationId, ExecutionText.Get("CurrentConversation")));
        panel.Children.Add(Label(ExecutionText.Format("InheritanceLine", detail.Field("effective").Text("scope"))));
        scopes.AddRange(detail.Array("workspaces").Select(workspace => new ExecutionChoice("workspace:" + workspace.Text("workspace_id"), workspace.Text("name") + " · " + workspace.Text("root"))));
        panel.Children.Add(Label(ExecutionText.Get("Scope"))); var scope = new ComboBox { ItemsSource = scopes, DisplayMemberPath = "Title", SelectedIndex = 0, MinHeight = 34 }; panel.Children.Add(scope);
        panel.Children.Add(Label(ExecutionText.Get("ExecutionMode"))); var mode = new ComboBox { ItemsSource = new[] { new ExecutionChoice("readonly", ExecutionText.Get("ModeReadOnlyChoice")), new ExecutionChoice("rules", ExecutionText.Get("ModeRulesChoice")), new ExecutionChoice("full", ExecutionText.Get("ModeFullChoice")) }, DisplayMemberPath = "Title", MinHeight = 34 }; panel.Children.Add(mode);
        AutomationProperties.SetAutomationId(scope, "PermissionScope"); AutomationProperties.SetAutomationId(mode, "PermissionMode");
        void SelectMode()
        {
            var selected = ((scope.SelectedItem as ExecutionChoice)?.Id ?? "global:").Split(':', 2);
            var name = policy.Text("global_mode");
            if (selected[0] == "conversation")
                foreach (var existing in policy.Array("scopes")) if (existing.Text("kind") == "workspace" && existing.Text("id") == detail.Text("workspace_id")) name = existing.Text("mode");
            foreach (var existing in policy.Array("scopes")) if (existing.Text("kind") == selected[0] && existing.Text("id") == selected[1]) name = existing.Text("mode");
            mode.SelectedItem = mode.Items.Cast<ExecutionChoice>().FirstOrDefault(item => item.Id == name) ?? mode.Items[1];
        }
        SelectMode(); scope.SelectionChanged += (_, _) => SelectMode();
        var enableRuleEdit = new CheckBox { Content = ExecutionText.Get("EditDangerousRules"), Margin = new Thickness(0, 14, 0, 5) }; panel.Children.Add(enableRuleEdit);
        panel.Children.Add(Label(ExecutionText.Get("RulesHelp")));
        var rules = new TextBox { Text = policy.Field("rules").Pretty(), AcceptsReturn = true, TextWrapping = TextWrapping.Wrap, Height = 210, VerticalScrollBarVisibility = ScrollBarVisibility.Auto, IsReadOnly = true, FontFamily = new FontFamily("Consolas"), Padding = new Thickness(8) }; panel.Children.Add(rules);
        enableRuleEdit.Checked += (_, _) => rules.IsReadOnly = false; enableRuleEdit.Unchecked += (_, _) => rules.IsReadOnly = true;
        var errorText = Label(""); errorText.SetResourceReference(TextBlock.ForegroundProperty, "DangerBrush"); panel.Children.Add(errorText);
        void LabelError(string message) { errorText.Text = message; }
        object? result = null; var save = Action(ExecutionText.Get("SaveAndApply"), "PermissionSave"); var cancel = Action(ExecutionText.Get("DialogCancel")); cancel.Click += (_, _) => ui.Window.Close();
        save.Click += (_, _) =>
        {
            if (mode.SelectedItem is not ExecutionChoice selectedMode || scope.SelectedItem is not ExecutionChoice selectedScope) return;
            if (selectedMode.Id == "full" && selectedScope.Id.StartsWith("conversation:", StringComparison.Ordinal)) { LabelError(ExecutionText.Get("FullRequiresWorkspace")); return; }
            if (selectedMode.Id == "full" && !Confirm(ui.Window, ExecutionText.Get("ConfirmFullTitle"), ExecutionText.Format("ConfirmFullPrompt", selectedScope.Title), ExecutionText.Get("Enable"))) return;
            try
            {
                var scopeParts = selectedScope.Id.Split(':', 2);
                var change = new Dictionary<string, object> { ["scope"] = scopeParts[0], ["scope_id"] = scopeParts[1], ["mode"] = selectedMode.Id, ["confirm_full"] = selectedMode.Id == "full", ["expected_revision"] = policy.Number("revision") };
                if (enableRuleEdit.IsChecked == true)
                {
                    using var parsed = JsonDocument.Parse(rules.Text);
                    if (parsed.RootElement.ValueKind != JsonValueKind.Array) throw new JsonException(ExecutionText.Get("RulesMustBeArray"));
                    change["rules"] = parsed.RootElement.Clone();
                }
                result = change; ui.Window.DialogResult = true;
            }
            catch (JsonException ex) { MessageBox.Show(ui.Window, ex.Message, ExecutionText.Get("RulesInvalid"), MessageBoxButton.OK, MessageBoxImage.Error); }
        };
        ui.Actions.Children.Add(cancel); ui.Actions.Children.Add(save); ui.Window.ShowDialog(); return result;
    }
    internal static bool Preferences(Window owner, ExecutionPreferences preferences)
    {
        var ui = Create(owner, ExecutionText.Get("PreferencesTitle"), 520, 390); var panel = new StackPanel(); ui.Root.Children.Add(panel);
        panel.Children.Add(Label(ExecutionText.Get("RetentionHelp")));
        var days = new TextBox { Text = preferences.RetentionDays.ToString(), MinHeight = 34, Padding = new Thickness(6) }; panel.Children.Add(days);
        panel.Children.Add(Label(ExecutionText.Get("FontSizeLabel"))); var font = new ComboBox { ItemsSource = new[] { 12d, 13d, 14d, 16d, 18d, 20d }, SelectedItem = preferences.FontSize, MinHeight = 34 }; panel.Children.Add(font);
        var notify = new CheckBox { Content = ExecutionText.Get("NotifyPending"), IsChecked = preferences.Notifications, Margin = new Thickness(0, 16, 0, 0) }; panel.Children.Add(notify);
        var saved = false; var save = Action(ExecutionText.Get("Save")); save.Click += (_, _) =>
        {
            if (!int.TryParse(days.Text, out var count) || count is < 1 or > 3650) { MessageBox.Show(ui.Window, ExecutionText.Get("RetentionInvalid"), ExecutionText.Get("RetentionInvalidTitle")); return; }
            preferences.RetentionDays = count; preferences.FontSize = font.SelectedItem is double size ? size : 14; preferences.Notifications = notify.IsChecked == true; saved = true; ui.Window.DialogResult = true;
        };
        ui.Actions.Children.Add(save); ui.Window.ShowDialog(); return saved;
    }
}
