using System.IO;
using System.Text.Json;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Input;
using System.Windows.Threading;
using Button = System.Windows.Controls.Button;
using Clipboard = System.Windows.Clipboard;
using ContextMenu = System.Windows.Controls.ContextMenu;
using ListBox = System.Windows.Controls.ListBox;
using MenuItem = System.Windows.Controls.MenuItem;
using KeyEventArgs = System.Windows.Input.KeyEventArgs;
using SaveFileDialog = Microsoft.Win32.SaveFileDialog;

namespace AgentDock.ControlPanel;

public partial class ExecutionWindow
{
    private readonly List<ExecutionObject> _managedItems = [];
    private int _dataOffset, _dataEpoch;
    private ulong _dataBefore;
    private bool _attentionView;

    private ContextMenu Menu(FrameworkElement anchor)
    {
        var menu = new ContextMenu { PlacementTarget = anchor, Resources = Resources }; anchor.ContextMenu = menu;
        return menu;
    }
    private static void AddMenu(ContextMenu menu, string title, Func<Task> action, bool enabled = true)
    {
        var item = new MenuItem { Header = title, IsEnabled = enabled };
        item.Click += async (_, _) => await action(); menu.Items.Add(item);
    }
    private void ActionMenu(ContextMenu menu, string title, Func<Task> action, bool enabled = true) => AddMenu(menu, title, () => GuardAsync(action), enabled);
    private static void Divider(ContextMenu menu) => menu.Items.Add(new Separator());
    private void OpenMenu(ContextMenu menu) { menu.IsOpen = true; }
    private static FrameworkElement Anchor(object sender, FrameworkElement fallback) => sender as FrameworkElement ?? fallback;
    private async Task SetConversationViewAsync(string view) { _conversationView = view; _frozenSelection = null; await LoadObjectsAsync(); SavePreferences(); }

    private void SidebarMenu_Click(object sender, RoutedEventArgs e)
    {
        var menu = Menu(Anchor(sender, ObjectsList));
        ActionMenu(menu, ExecutionText.Get("CurrentConversations"), () => SetConversationViewAsync("active"));
        ActionMenu(menu, ExecutionText.Get("ArchivedView"), () => SetConversationViewAsync("archived"));
        ActionMenu(menu, ExecutionText.Get("TrashView"), () => SetConversationViewAsync("trash")); Divider(menu);
        ActionMenu(menu, ExecutionText.Get("SelectAllFiltered"), SelectAllObjectsAsync);
        ActionMenu(menu, ExecutionText.Get("ManageSelectedConversations"), () => { ShowObjectMenu(ObjectsList, SelectedObjectIds()); return Task.CompletedTask; }, ObjectsList.SelectedItems.Count > 0);
        ActionMenu(menu, ExecutionText.Get("HistoricalTasksAndUnattributed"), () => OpenDataManagerAsync(false));
        OpenMenu(menu);
    }
    private string[] SelectedObjectIds() => _frozenSelection ?? ObjectsList.SelectedItems.Cast<ExecutionObject>().Where(item => !item.IsUnknown && !item.IsOrphan).Select(item => item.Id).Distinct().ToArray();
    private async Task SelectAllObjectsAsync()
    {
        var page = await _client.ExecutionGetAsync("/internal/runtime/conversations?" + ListQuery(true), _lifetime.Token);
        _frozenSelection = page.Array("selected_ids").Select(value => value.GetString()!).Where(value => value.Length > 0).ToArray();
        _updating = true;
        try { ObjectsList.SelectAll(); } finally { _updating = false; }
        Warn(ExecutionText.Format("SelectedConversationCount", _frozenSelection.Length));
    }
    private async void SelectAllObjects_Click(object sender, RoutedEventArgs e) => await GuardAsync(SelectAllObjectsAsync);
    private void Objects_RightClick(object sender, MouseButtonEventArgs e)
    {
        if (Ancestor<ListBoxItem>(e.OriginalSource as DependencyObject) is not { DataContext: ExecutionObject row } item) return;
        if (!item.IsSelected) { _frozenSelection = null; ObjectsList.SelectedItem = row; }
        _menuSelection = SelectedObjectIds(); ShowObjectMenu(item, _menuSelection); e.Handled = true;
    }
    private void ObjectMore_Click(object sender, RoutedEventArgs e)
    {
        if ((sender as FrameworkElement)?.DataContext is not ExecutionObject item) return;
        _frozenSelection = null; ObjectsList.SelectedItem = item;
        _menuSelection = item.IsUnknown || item.IsOrphan ? [] : [item.Id];
        ShowObjectMenu((FrameworkElement)sender, _menuSelection); e.Handled = true;
    }
    private void ConversationMenu_Click(object sender, RoutedEventArgs e)
    {
        var ids = _selected is { IsUnknown:false, IsOrphan:false } ? new[] { _selected.Id } : Array.Empty<string>();
        _menuSelection = ids; ShowObjectMenu(Anchor(sender, ConversationHeader), ids);
    }
    private void ShowObjectMenu(FrameworkElement anchor, string[] ids)
    {
        var fixedIds = ids.ToArray(); var selected = Objects.FirstOrDefault(item => fixedIds.Contains(item.Id)) ?? _selected;
        var menu = Menu(anchor);
        ActionMenu(menu, ExecutionText.Get("ConversationDetails"), () => { ShowInfo(ExecutionText.Get("ConversationDetails"), selected?.Snapshot.Pretty() ?? ExecutionText.Get("UnattributedManagedByCallId")); return Task.CompletedTask; }, selected is not null);
        ActionMenu(menu, ExecutionText.Get("ExportExecutionRecords"), () => ExportConversationsAsync(fixedIds), selected is not null);
        if (selected is { IsUnknown:true } || selected is { IsOrphan:true })
        {
            ActionMenu(menu, ExecutionText.Get("ManageRecords"), () => OpenDataManagerAsync(false)); OpenMenu(menu); return;
        }
        Divider(menu);
        ActionMenu(menu, ExecutionText.Get("Rename"), () => RenameAsync("conversation", fixedIds, selected?.Title ?? ""), fixedIds.Length == 1);
        ActionMenu(menu, ExecutionText.Get("Tags"), () => TagsAsync("conversation", fixedIds, selected?.Tags ?? ""), fixedIds.Length > 0);
        ActionMenu(menu, selected?.Pinned == true ? ExecutionText.Get("Unpin") : ExecutionText.Get("Pin"), () => BatchAsync("conversation", fixedIds, selected?.Pinned == true ? "unpin" : "pin"), fixedIds.Length > 0);
        ActionMenu(menu, selected?.Archived == true ? ExecutionText.Get("Unarchive") : ExecutionText.Get("Archive"), () => BatchAsync("conversation", fixedIds, selected?.Archived == true ? "unarchive" : "archive"), fixedIds.Length > 0);
        ActionMenu(menu, selected?.Trashed == true ? ExecutionText.Get("RestoreFromTrash") : ExecutionText.Get("MoveToTrash"), () => ConfirmBatchAsync("conversation", fixedIds, selected?.Trashed == true ? "restore" : "trash"), fixedIds.Length > 0);
        if (selected?.Trashed == true) ActionMenu(menu, ExecutionText.Get("DeletePermanently"), () => ConfirmBatchAsync("conversation", fixedIds, "delete"), fixedIds.Length > 0);
        Divider(menu);
        var terminated = selected?.Terminated == true || selected?.Id == _selected?.Id && _conversationSnapshot.HasDate("terminated_at");
        ActionMenu(menu, terminated ? ExecutionText.Get("ResumeConversation") : ExecutionText.Get("TerminateConversation"), () => ChangeLifecycleAsync(fixedIds[0], terminated ? "resume" : "terminate"), fixedIds.Length == 1 && selected?.Trashed != true);
        ActionMenu(menu, ExecutionText.Get("LinkExistingTask"), () => LinkTaskAsync(fixedIds[0]), fixedIds.Length == 1 && !terminated && selected?.Trashed != true);
        OpenMenu(menu);
    }
    private async Task RenameAsync(string kind, string[] ids, string title)
    {
        if (ids.Length != 1) return;
        var value = ExecutionDialogs.Prompt(this, ExecutionText.Get("Rename"), ExecutionText.Get("EnterName"), title);
        if (value is null) return;
        if (value.Length == 0) { Warn(ExecutionText.Get("NameRequired")); return; }
        await BatchAsync(kind, ids, "rename", value);
    }
    private async Task TagsAsync(string kind, string[] ids, string initial)
    {
        var value = ExecutionDialogs.Prompt(this, ExecutionText.Get("Tags"), ExecutionText.Get("TagPrompt"), initial);
        if (value is null) return;
        // Historical fullwidth and ideographic commas remain accepted separators.
        var tags = value.Split([',', '\uFF0C', '\u3001'], StringSplitOptions.TrimEntries | StringSplitOptions.RemoveEmptyEntries).Distinct().ToArray();
        await BatchAsync(kind, ids, "tags", tags: tags);
    }
    private async Task ConfirmBatchAsync(string kind, string[] ids, string action)
    {
        if (ids.Length == 0) return;
        if (action is "trash" or "delete")
        {
            var prompt = action == "delete" ? ExecutionText.Format("PermanentDeletePrompt", ids.Length) : ExecutionText.Format("TrashPrompt", ids.Length);
            if (!ExecutionDialogs.Confirm(this, action == "delete" ? ExecutionText.Get("DeletePermanently") : ExecutionText.Get("MoveToTrash"), prompt, action == "delete" ? ExecutionText.Get("DeletePermanently") : ExecutionText.Get("MoveToTrash"))) return;
        }
        await BatchAsync(kind, ids, action);
    }
    private async Task BatchAsync(string kind, string[] ids, string action, string title = "", string[]? tags = null)
    {
        var endpoint = kind switch { "task" => "/internal/runtime/tasks/batch", "call" => "/internal/runtime/calls/batch", _ => "/internal/runtime/conversations/batch" };
        var outcomes = new List<JsonElement>(); long succeeded = 0, failed = 0, skipped = 0;
        foreach (var batch in ids.Distinct().Chunk(200))
        {
            var result = await _client.ExecutionPostAsync(endpoint, new { ids = batch, action, title, tags = tags ?? [], retention_days = _preferences.RetentionDays, confirm_permanent = action == "delete" }, _lifetime.Token);
            succeeded += result.Number("succeeded"); failed += result.Number("failed"); skipped += result.Number("skipped"); outcomes.AddRange(result.Array("items"));
        }
        _frozenSelection = null;
        await LoadObjectsAsync();
        if (_selected is not null) await LoadCallsAsync(false);
        if (DataManagementPanel.Visibility == Visibility.Visible) await LoadManagedAsync(false);
        if (failed > 0 || skipped > 0) ShowInfo(ExecutionText.Get("ManagementResultTitle"), ExecutionText.Format("ManagementResult", succeeded, skipped, failed, string.Join("\n", outcomes.Where(item => item.Text("status") != "succeeded").Select(item => ExecutionText.Format("IdMessage", item.Text("id"), item.Text("message"))))));
        else Warn(ExecutionText.Format("RecordsUpdated", succeeded));
    }
    private async Task ChangeLifecycleAsync(string id, string action)
    {
        var stopping = action == "terminate";
        if (!ExecutionDialogs.Confirm(this, stopping ? ExecutionText.Get("TerminateConversation") : ExecutionText.Get("ResumeConversation"), stopping ? ExecutionText.Get("TerminatePrompt") : ExecutionText.Get("ResumePrompt"), stopping ? ExecutionText.Get("Terminate") : ExecutionText.Get("Resume"))) return;
        var result = await _client.ExecutionPostAsync("/internal/runtime/conversations/" + Escape(id) + "/" + action, new { confirm = true }, _lifetime.Token);
        if (_selected?.Id == id) _conversationSnapshot = result.Field("conversation");
        await LoadObjectsAsync(); await LoadCallsAsync(false); UpdateStopButton();
        var warnings = result.Array("warnings").Select(value => value.GetString()).Where(value => !string.IsNullOrWhiteSpace(value));
        var remaining = result.Flag("has_remaining_activity");
        var text = remaining ? ExecutionText.Get("TerminationRemaining") : stopping ? ExecutionText.Get("ConversationTerminatedShort") : ExecutionText.Get("ConversationResumed");
        Warn(ExecutionText.Format("WarningWithDetails", text, string.Join("\n", warnings.Prepend(""))), stopping && !remaining ? WarningKind.ConversationTerminated : WarningKind.Other);
    }
    private async void Terminate_Click(object sender, RoutedEventArgs e) { if (_selected is { } selected) await GuardAsync(() => ChangeLifecycleAsync(selected.Id, "terminate")); }
    private async Task LinkTaskAsync(string conversation)
    {
        var page = await _client.ExecutionGetAsync("/internal/runtime/execution/tasks?view=active&limit=200", _lifetime.Token);
        var choices = page.Array("tasks").Select(value => new ExecutionChoice(value.Text("id", value.Text("task_id")), value.Text("title"))).ToArray();
        var id = ExecutionDialogs.Choose(this, ExecutionText.Get("LinkExistingTask"), ExecutionText.Get("LinkTaskExplanation"), choices);
        if (id is null) return;
        await _client.ExecutionPostAsync("/internal/runtime/conversations/" + Escape(conversation) + "/link-task", new { task_id = id }, _lifetime.Token);
        if (_selected is not null) await SelectObjectAsync(_selected);
    }
    private async void Permissions_Click(object sender, RoutedEventArgs e) => await GuardAsync(async () =>
    {
        var query = _selected is { IsUnknown:false, IsOrphan:false } ? "?conversation_id=" + Escape(_selected.Id) : "";
        var detail = await _client.ExecutionGetAsync("/internal/runtime/permissions/effective" + query, _lifetime.Token);
        var change = ExecutionDialogs.Permissions(this, detail);
        if (change is not null) { await _client.ExecutionPostAsync("/internal/runtime/permissions", change, _lifetime.Token); await RefreshOverviewAsync(); Warn(ExecutionText.Get("PermissionsSaved")); }
    });
    private async void Connection_Click(object sender, RoutedEventArgs e) => await GuardAsync(async () =>
    {
        ConnectionButton.IsEnabled = false;
        try
        {
            var value = await _client.ExecutionGetAsync("/internal/runtime/execution/connection", _lifetime.Token);
            var text = ExecutionText.Format("LocalStreamLine", _streamConnected ? ExecutionText.Get("StreamConnected") : ExecutionText.Get("StreamReconnecting")) + "\n" + value.Text("summary") + "\n" + value.Text("detail");
            ShowInfo(ExecutionText.Get("Connection"), text + "\n\n" + ExecutionText.Get("CheckingPublicAccess"), DetailsKind.Connection);
            var manifestPath = Path.Combine(_runtime.RuntimeRoot, "runtime.json");
            var publicState = ExecutionText.Get("PublicAddressNotConfigured");
            if (File.Exists(manifestPath))
            {
                if (new FileInfo(manifestPath).Length > 1048576) throw new IOException(ExecutionText.Get("RuntimeConfigTooLarge"));
                using var manifest = JsonDocument.Parse(await File.ReadAllTextAsync(manifestPath, _lifetime.Token));
                var origin = manifest.RootElement.Text("public_url", manifest.RootElement.Text("public_access_url"));
                if (!string.IsNullOrWhiteSpace(origin))
                {
                    var probe = await _runtime.TestUrlAsync(origin, _lifetime.Token);
                    publicState = ExecutionText.Format("PublicStateBlock", probe.Success ? ExecutionText.Get("PublicReachable") : ExecutionText.Get("PublicUnreachable"), probe.Message);
                }
            }
            // Public discovery is independent of the latest authenticated client
            // evidence. A successful anonymous probe never means reauthorization.
            value = await _client.ExecutionGetAsync("/internal/runtime/execution/connection", _lifetime.Token);
            if (InfoDetailsText.Visibility == Visibility.Visible && _detailsKind == DetailsKind.Connection)
                InfoDetailsText.Text = ExecutionText.Format("PublicStateBlock", ExecutionText.Format("LocalStreamLine", _streamConnected ? ExecutionText.Get("StreamConnected") : ExecutionText.Get("StreamReconnecting")) + "\n" + value.Text("summary") + "\n" + value.Text("detail"), publicState);
        }
        finally { ConnectionButton.IsEnabled = true; }
    });
    private void Theme_Click(object sender, RoutedEventArgs e)
    {
        var menu = Menu(Anchor(sender, ConversationHeader));
        foreach (var choice in new[] { ("system", ExecutionText.Get("ThemeSystem")), ("light", ExecutionText.Get("ThemeLight")), ("dark", ExecutionText.Get("ThemeDark")) })
            ActionMenu(menu, choice.Item2, () => { ApplyTheme(choice.Item1); SavePreferences(); return Task.CompletedTask; });
        OpenMenu(menu);
    }
    private void SettingsMenu_Click(object sender, RoutedEventArgs e)
    {
        var menu = Menu(Anchor(sender, ConversationHeader));
        ActionMenu(menu, ExecutionText.Get("DisplayRetentionNotifications"), () => { if (ExecutionDialogs.Preferences(this, _preferences)) { FontSize = _preferences.FontSize; SavePreferences(); } return Task.CompletedTask; });
        ActionMenu(menu, ExecutionText.Get("HistoricalTaskManagement"), () => OpenDataManagerAsync(false));
        ActionMenu(menu, ExecutionText.Get("SaveCurrentFilter"), () => { var name = ExecutionDialogs.Prompt(this, ExecutionText.Get("SaveFilterTitle"), ExecutionText.Get("FilterName"), ""); if (!string.IsNullOrWhiteSpace(name)) { _preferences.SavedFilters[name] = [_conversationView, SearchBox.Text, CallSearchBox.Text, ComboValue(CallStatusCombo)]; SavePreferences(); } return Task.CompletedTask; });
        foreach (var pair in _preferences.SavedFilters.ToArray())
            ActionMenu(menu, ExecutionText.Format("SavedFilter", pair.Key), async () => { var values = pair.Value; if (values.Length != 4) return; _conversationView = values[0]; SearchBox.Text = values[1]; CallSearchBox.Text = values[2]; CallStatusCombo.SelectedItem = CallStatusCombo.Items.Cast<ComboBoxItem>().FirstOrDefault(item => item.Tag?.ToString() == values[3]); await LoadObjectsAsync(); });
        ActionMenu(menu, ExecutionText.Get("StructureHelpTitle"), () => { ShowInfo(ExecutionText.Get("StructureHelpTitle"), ExecutionText.Get("StructureHelp")); return Task.CompletedTask; });
        OpenMenu(menu);
    }
    private string[] SelectedCallIds() => CallsList.SelectedItems.Cast<ExecutionCallRow>().Select(row => row.Id).Distinct().ToArray();
    private void Calls_RightClick(object sender, MouseButtonEventArgs e)
    {
        if (Ancestor<ListBoxItem>(e.OriginalSource as DependencyObject) is not { DataContext: ExecutionCallRow row } item) return;
        if (!item.IsSelected) CallsList.SelectedItem = row;
        ShowCallMenu(item, SelectedCallIds()); e.Handled = true;
    }
    private void CallMenu_Click(object sender, RoutedEventArgs e) => ShowCallMenu(Anchor(sender, CallsList), SelectedCallIds());
    private void ShowCallMenu(FrameworkElement anchor, string[] ids)
    {
        var fixedIds = ids.ToArray(); var menu = Menu(anchor);
        ActionMenu(menu, ExecutionText.Get("ExportCurrentFilter"), () => ExportScopeAsync(CallScopeQuery(), ExecutionText.Get("ExecutionRecords")));
        ActionMenu(menu, ExecutionText.Get("ExportSelectedRecords"), () => ExportCallIdsAsync(fixedIds), fixedIds.Length > 0);
        ActionMenu(menu, ExecutionText.Get("ArchiveSelected"), () => BatchAsync("call", fixedIds, "archive"), fixedIds.Length > 0);
        ActionMenu(menu, ExecutionText.Get("IsolateSelected"), () => BatchAsync("call", fixedIds, "isolate"), fixedIds.Length > 0);
        ActionMenu(menu, ExecutionText.Get("MoveToTrash"), () => ConfirmBatchAsync("call", fixedIds, "trash"), fixedIds.Length > 0); Divider(menu);
        foreach (var view in new[] { ("active", ExecutionText.Get("CurrentRecords")), ("archived", ExecutionText.Get("ArchivedRecords")), ("isolated", ExecutionText.Get("IsolatedRecords")), ("trash", ExecutionText.Get("TrashRecords")) })
            ActionMenu(menu, view.Item2, async () => { _callView = view.Item1; await LoadCallsAsync(false); });
        if (_callView == "trash") { ActionMenu(menu, ExecutionText.Get("RestoreSelected"), () => BatchAsync("call", fixedIds, "restore"), fixedIds.Length > 0); ActionMenu(menu, ExecutionText.Get("DeleteSelectedPermanently"), () => ConfirmBatchAsync("call", fixedIds, "delete"), fixedIds.Length > 0); }
        if (_callView == "isolated") ActionMenu(menu, ExecutionText.Get("UnisolateSelected"), () => BatchAsync("call", fixedIds, "unisolate"), fixedIds.Length > 0);
        if (_callView == "archived") ActionMenu(menu, ExecutionText.Get("UnarchiveSelected"), () => BatchAsync("call", fixedIds, "unarchive"), fixedIds.Length > 0);
        OpenMenu(menu);
    }
    private void CopyCommand_Click(object sender, RoutedEventArgs e) { if (_detailCall is { } row) CopyText(row.Command); }
    private void CopyOutput_Click(object sender, RoutedEventArgs e) { if (_detailCall is { } row) CopyText(row.Output); }
    private void CopyText(string value) { try { Clipboard.SetText(value); } catch (System.Runtime.InteropServices.ExternalException) { Warn(ExecutionText.Get("ClipboardBusy")); } }
    private async void StopCall_Click(object sender, RoutedEventArgs e) { if (_detailCall is { CanStop:true } row) await GuardAsync(async () => { await _client.ExecutionPostAsync("/internal/runtime/calls/" + Escape(row.Id) + "/stop", new { }, _lifetime.Token); await LoadCallDetailAsync(row); }); }
    private async void Approve_Click(object sender, RoutedEventArgs e)
    {
        if (_detailCall is not { NeedsApproval:true } row) return;
        await GuardAsync(async () =>
        {
            var detail = await _client.ExecutionGetAsync("/internal/runtime/approvals/" + Escape(row.ApprovalId), _lifetime.Token);
            var decision = ExecutionDialogs.Approve(this, detail);
            if (decision is null) return;
            await _client.ExecutionPostAsync("/internal/runtime/approvals/" + Escape(row.ApprovalId) + "/" + decision.Value.Action, new { allow_workspace = decision.Value.AllowWorkspace }, _lifetime.Token);
            await LoadCallDetailAsync(row); await RefreshOverviewAsync();
        });
    }
    private async void Reject_Click(object sender, RoutedEventArgs e) { if (_detailCall is { NeedsApproval:true } row) await GuardAsync(async () => { await _client.ExecutionPostAsync("/internal/runtime/approvals/" + Escape(row.ApprovalId) + "/reject", new { }, _lifetime.Token); await LoadCallDetailAsync(row); await RefreshOverviewAsync(); }); }
    private void RetryInstruction_Click(object sender, RoutedEventArgs e)
    {
        if (_detailCall is not { CanRetry:true } row) return;
        var text = ExecutionText.Format("RetryInstructionBody", row.Id);
        CopyText(text); ShowInfo(ExecutionText.Get("RetryInstructionCopiedTitle"), text);
    }
    private async void ContinueBranch_Click(object sender, RoutedEventArgs e)
    {
        if (_selectedTaskId.Length == 0 || _branch.Length == 0) return;
        if (!ExecutionDialogs.Confirm(this, ExecutionText.Get("SwitchBranchTitle"), ExecutionText.Get("SwitchBranchPrompt"), ExecutionText.Get("Switch"))) return;
        await GuardAsync(async () => { await _client.ControlAsync(new { action = "thread_switch", task_id = _selectedTaskId, thread_id = _branch }, _lifetime.Token); Warn(ExecutionText.Get("ContinuationBranchUpdated")); });
    }
    private void ContinueTask_Click(object sender, RoutedEventArgs e) { if (_selectedTaskId.Length > 0) { var text = ExecutionText.Format("ContinueTaskBody", _selectedTaskId, _branch); CopyText(text); Warn(ExecutionText.Get("ContinueTaskCopied")); } }
    private void TaskMenu_Click(object sender, RoutedEventArgs e)
    {
        if (_selectedTaskId.Length == 0) return;
        var id = _selectedTaskId; var menu = Menu(Anchor(sender, TaskDetailsPanel));
        ActionMenu(menu, ExecutionText.Get("SetCurrentTask"), async () => { if (_selected is null || _selected.IsUnknown) return; await _client.ExecutionPostAsync("/internal/runtime/conversations/" + Escape(_selected.Id) + "/current-task", new { task_id = id, task_thread_id = _branch, binding_revision = _conversationSnapshot.Field("state").Number("binding_revision") }, _lifetime.Token); await SelectObjectAsync(_selected); });
        ActionMenu(menu, ExecutionText.Get("RenameTask"), () => RenameAsync("task", [id], _taskSnapshot.Text("title")));
        ActionMenu(menu, ExecutionText.Get("Tags"), () => TagsAsync("task", [id], ""));
        ActionMenu(menu, ExecutionText.Get("ArchiveTask"), () => BatchAsync("task", [id], "archive"));
        ActionMenu(menu, ExecutionText.Get("CancelTask"), async () => { var reason = ExecutionDialogs.Prompt(this, ExecutionText.Get("CancelTask"), ExecutionText.Get("CancelReason"), ""); if (string.IsNullOrWhiteSpace(reason)) return; await _client.ControlAsync(new { action = "cancel", task_id = id, summary = reason }, _lifetime.Token); await LoadTaskAsync(id, _branch, true); });
        ActionMenu(menu, ExecutionText.Get("MoveToTrash"), () => ConfirmBatchAsync("task", [id], "trash")); OpenMenu(menu);
    }
    private async Task OpenDataManagerAsync(bool attention)
    {
        _attentionView = attention; OpenDetails(attention ? ExecutionText.Get("AttentionTitle") : ExecutionText.Get("HistoricalTaskManagement"), DataManagementPanel, DetailsKind.Data);
        if (attention) { _updating = true; DataKindCombo.SelectedIndex = 2; DataViewCombo.SelectedIndex = 0; _updating = false; }
        await LoadManagedAsync(false);
    }
    private async void Attention_Click(object sender, RoutedEventArgs e) => await GuardAsync(() => OpenDataManagerAsync(true));
    private async void DataFilter_Changed(object sender, SelectionChangedEventArgs e) { if (_initialized && !_updating && DataManagementPanel.Visibility == Visibility.Visible) { _attentionView = false; DetailsTitle.Text = ExecutionText.Get("HistoricalTaskManagement"); await GuardAsync(() => LoadManagedAsync(false)); } }
    private async Task LoadManagedAsync(bool more)
    {
        var epoch = ++_dataEpoch; var kind = ComboValue(DataKindCombo); var view = ComboValue(DataViewCombo); var tasks = kind == "task";
        if (tasks && view == "isolated") { _managedItems.Clear(); ManagedObjectsList.ItemsSource = _managedItems.ToArray(); MoreDataButton.Visibility = Visibility.Collapsed; return; }
        var query = tasks ? $"/internal/runtime/execution/tasks?view={view}&limit=200&offset={(more ? _dataOffset : 0)}" : $"/internal/runtime/calls?view={view}&limit=200&top_level=true" + (kind == "unattributed" ? "&unattributed=true" : "") + (_attentionView ? "&status=pending_approval" : "") + (more && _dataBefore > 0 ? "&before=" + _dataBefore : "");
        var page = await _client.ExecutionGetAsync(query, _lifetime.Token);
        if (_closed || epoch != _dataEpoch) return;
        if (!more) _managedItems.Clear();
        foreach (var raw in page.Array(tasks ? "tasks" : "calls"))
        {
            var item = tasks ? ExecutionObject.From(raw, "task") : new ExecutionObject { Id = raw.Text("call_id"), Kind = "call", Title = new ExecutionCallRow(raw).Title, Detail = ExecutionJson.State(raw.Text("status")), Archived = raw.HasDate("archived_at"), Trashed = raw.HasDate("trashed_at"), Snapshot = raw.Clone() };
            if (_managedItems.All(existing => existing.Id != item.Id)) _managedItems.Add(item);
        }
        _dataOffset = (int)page.Number("next_offset"); _dataBefore = (ulong)page.Number("next_before");
        MoreDataButton.Visibility = page.Flag("has_more") ? Visibility.Visible : Visibility.Collapsed;
        ManagedObjectsList.ItemsSource = _managedItems.ToArray();
    }
    private async void MoreData_Click(object sender, RoutedEventArgs e) => await GuardAsync(() => LoadManagedAsync(true));
    private void Data_RightClick(object sender, MouseButtonEventArgs e) { if (Ancestor<ListBoxItem>(e.OriginalSource as DependencyObject) is not { DataContext:ExecutionObject row } item) return; if (!item.IsSelected) ManagedObjectsList.SelectedItem = row; ShowDataMenu(item); e.Handled = true; }
    private void ManageData_Click(object sender, RoutedEventArgs e) => ShowDataMenu(Anchor(sender, ManagedObjectsList));
    private void ShowDataMenu(FrameworkElement anchor)
    {
        var rows = ManagedObjectsList.SelectedItems.Cast<ExecutionObject>().ToArray(); if (rows.Length == 0) { Warn(ExecutionText.Get("SelectRecordsFirst")); return; }
        var ids = rows.Select(row => row.Id).ToArray(); var kind = rows[0].Kind; var view = ComboValue(DataViewCombo); var menu = Menu(anchor);
        ActionMenu(menu, ExecutionText.Get("ViewDetails"), async () => { if (kind == "task") { _selectedTaskId = ids[0]; OpenDetails(rows[0].Title, TaskDetailsPanel, DetailsKind.Task); await LoadTaskAsync(ids[0], "", true); } else { var row = new ExecutionCallRow(rows[0].Snapshot); _detailCall = row; CallDetailsTabs.DataContext = row; CallDetailsTabs.SelectedIndex = 0; OpenDetails(row.Title, CallDetailsTabs, DetailsKind.Call); await LoadCallDetailAsync(row); } }, ids.Length == 1);
        if (kind == "task") ActionMenu(menu, ExecutionText.Get("Rename"), () => RenameAsync(kind, ids, rows[0].Title), ids.Length == 1);
        ActionMenu(menu, view == "archived" ? ExecutionText.Get("Unarchive") : ExecutionText.Get("Archive"), () => BatchAsync(kind, ids, view == "archived" ? "unarchive" : "archive"));
        if (kind == "call") ActionMenu(menu, view == "isolated" ? ExecutionText.Get("Unisolate") : ExecutionText.Get("Isolate"), () => BatchAsync(kind, ids, view == "isolated" ? "unisolate" : "isolate"));
        ActionMenu(menu, view == "trash" ? ExecutionText.Get("Restore") : ExecutionText.Get("MoveToTrash"), () => ConfirmBatchAsync(kind, ids, view == "trash" ? "restore" : "trash"));
        if (view == "trash") ActionMenu(menu, ExecutionText.Get("DeletePermanently"), () => ConfirmBatchAsync(kind, ids, "delete"));
        OpenMenu(menu);
    }
    private async void ExportData_Click(object sender, RoutedEventArgs e) => await GuardAsync(async () =>
    {
        var rows = ManagedObjectsList.SelectedItems.Cast<ExecutionObject>().ToArray();
        if (rows.Length == 0) { Warn(ExecutionText.Get("SelectExportFirst")); return; }
        if (rows[0].Kind == "call") await ExportCallIdsAsync(rows.Select(row => row.Id).ToArray());
        else await SaveExportAsync(ExecutionText.Get("TaskRecords"), rows.Select(row => row.Snapshot).ToArray());
    });
    private async Task<List<JsonElement>> ReadScopeAsync(string query)
    {
        var records = new List<JsonElement>(); ulong before = 0;
        while (true)
        {
            var page = await _client.ExecutionGetAsync("/internal/runtime/calls?" + query + "&limit=200&include_output=true" + (before > 0 ? "&before=" + before : ""), _lifetime.Token);
            records.AddRange(page.Array("calls"));
            if (!page.Flag("has_more")) return records;
            var next = (ulong)page.Number("next_before"); if (next == 0 || next == before) throw new IOException(ExecutionText.Get("ExportPageDidNotAdvance"));
            if (records.Count >= 100000) throw new IOException(ExecutionText.Get("ExportTooLarge")); before = next;
        }
    }
    private async Task ExportScopeAsync(string query, string title) => await SaveExportAsync(title, await ReadScopeAsync(query));
    private async Task ExportConversationsAsync(string[] ids)
    {
        var calls = new List<JsonElement>();
        if (ids.Length == 0 && _selected?.IsUnknown == true) calls = await ReadScopeAsync("unattributed=true&view=all");
        foreach (var id in ids) calls.AddRange(await ReadScopeAsync("conversation_id=" + Escape(id) + "&view=all"));
        await SaveExportAsync(ExecutionText.Get("ConversationExecutionRecords"), calls);
    }
    private async Task ExportCallIdsAsync(string[] ids)
    {
        var calls = new List<JsonElement>();
        foreach (var id in ids) calls.Add(await _client.ExecutionGetAsync("/internal/runtime/calls/" + Escape(id), _lifetime.Token));
        await SaveExportAsync(ExecutionText.Get("SelectedExecutionRecords"), calls);
    }
    private async Task SaveExportAsync(string title, object records)
    {
        var dialog = new SaveFileDialog { Title = ExecutionText.Format("ExportDialogTitle", title), Filter = ExecutionText.Get("JsonFileFilter"), FileName = "AgentDock-" + title + "-" + DateTime.Now.ToString("yyyyMMdd-HHmm") + ".json", AddExtension = true, OverwritePrompt = true };
        if (dialog.ShowDialog(this) != true) return;
        var temporary = dialog.FileName + "." + Guid.NewGuid().ToString("N") + ".tmp";
        try { await File.WriteAllTextAsync(temporary, JsonSerializer.Serialize(new { schema_version = 2, exported_at = DateTimeOffset.UtcNow, records }, ActivityClient.JsonOptions), _lifetime.Token); File.Move(temporary, dialog.FileName, true); Warn(ExecutionText.Format("ExportedPath", dialog.FileName)); }
        finally { if (File.Exists(temporary)) File.Delete(temporary); }
    }
    private async void Window_KeyDown(object sender, KeyEventArgs e)
    {
        if (e.Key == Key.Escape) { CloseDetails(); e.Handled = true; }
        else if (e.Key == Key.F && Keyboard.Modifiers.HasFlag(ModifierKeys.Control)) { SearchBox.Focus(); SearchBox.SelectAll(); e.Handled = true; }
        else if (e.Key == Key.F5) { await GuardAsync(async () => { await LoadObjectsAsync(); await LoadCallsAsync(false); await RefreshOverviewAsync(); }); e.Handled = true; }
        else if (e.Key == Key.F10 && Keyboard.Modifiers.HasFlag(ModifierKeys.Shift)) { if (ObjectsList.IsKeyboardFocusWithin) ShowObjectMenu(ObjectsList, SelectedObjectIds()); else if (CallsList.IsKeyboardFocusWithin) ShowCallMenu(CallsList, SelectedCallIds()); else if (ManagedObjectsList.IsKeyboardFocusWithin) ShowDataMenu(ManagedObjectsList); e.Handled = true; }
    }
}
