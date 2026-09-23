using System.Collections.ObjectModel;
using System.ComponentModel;
using System.Text.Json;

namespace AgentDock.ControlPanel;

public static class ExecutionJson
{
    public static JsonElement Field(this JsonElement value, string name) => value.ValueKind == JsonValueKind.Object && value.TryGetProperty(name, out var field) ? field : default;
    public static string Text(this JsonElement value, string name, string fallback = "") => value.Field(name).ValueKind == JsonValueKind.String ? value.Field(name).GetString() ?? fallback : fallback;
    public static long Number(this JsonElement value, string name) => value.Field(name).ValueKind == JsonValueKind.Number && value.Field(name).TryGetInt64(out var number) ? number : 0;
    public static long? OptionalNumber(this JsonElement value, string name) => value.Field(name).ValueKind == JsonValueKind.Number && value.Field(name).TryGetInt64(out var number) ? number : null;
    public static DateTimeOffset? Date(this JsonElement value, string name) => DateTimeOffset.TryParse(value.Text(name), out var date) && date.Year > 1 ? date : null;
    public static bool Flag(this JsonElement value, string name) => value.Field(name).ValueKind == JsonValueKind.True;
    public static JsonElement[] Array(this JsonElement value, string name) => value.Field(name).ValueKind == JsonValueKind.Array ? value.Field(name).EnumerateArray().Select(item => item.Clone()).ToArray() : [];
    public static string Pretty(this JsonElement value) => value.ValueKind == JsonValueKind.Undefined ? "" : JsonSerializer.Serialize(value, new JsonSerializerOptions { WriteIndented = true });
    public static string State(string state) => state switch
    {
        "created" => UiText.Get("ExecutionWaiting"), "running" or "in_progress" => UiText.Get("ExecutionRunning"), "pending_approval" => UiText.Get("ExecutionPendingApproval"), "succeeded" or "completed" => UiText.Get("ExecutionCompleted"),
        "failed" => UiText.Get("ExecutionFailed"), "partial" => UiText.Get("ExecutionPartial"), "cancelled" => UiText.Get("ExecutionCancelled"), "unknown" => UiText.Get("ExecutionUnknownResult"), "blocked" => UiText.Get("ExecutionBlocked"), "pending" => UiText.Get("ExecutionNotStarted"), _ => state
    };
    public static string ApprovalState(string state) => state switch
    {
        "pending" => UiText.Get("ExecutionPendingApproval"), "approved" => UiText.Get("ExecutionApprovalApproved"),
        "rejected" => UiText.Get("ExecutionApprovalRejected"), "cancelled" => UiText.Get("ExecutionCancelled"),
        "expired" => UiText.Get("ExecutionApprovalExpired"), _ => state
    };
    public static string Mode(string mode) => mode switch { "full" => UiText.Get("ExecutionFullPermission"), "readonly" or "read_only" => UiText.Get("ExecutionReadOnly"), "rules" or "ask" or "guarded" or "default" => UiText.Get("ExecutionApprovalRequired"), _ => mode };
    public static bool HasDate(this JsonElement value, string name) => value.Field(name).ValueKind == JsonValueKind.String;
}

public sealed class WorkspaceGroupKey(string id, string title) : INotifyPropertyChanged
{
    public string Id { get; } = id;
    public string Title { get; private set; } = title;
    public string Root { get; private set; } = "";
    public int Total { get; private set; }
    public DateTimeOffset? LastActivityAt { get; private set; }
    public event PropertyChangedEventHandler? PropertyChanged;
    public void Apply(JsonElement value)
    {
        // Only server-owned fallback codes are localized; user workspace names stay data.
        Title = value.Text("title_source") switch
        {
            "historical_workspace" => UiText.Get("ExecutionHistoricalWorkspace"),
            "unattributed" => UiText.Get("ExecutionUnattributedGroup"),
            "unassigned" => UiText.Get("ExecutionUnassignedProject"),
            _ => value.Text("title", Title)
        };
        Root = value.Text("root");
        Total = (int)value.Number("total"); LastActivityAt = value.Date("last_activity_at");
        PropertyChanged?.Invoke(this, new(null));
    }
    public override bool Equals(object? value) => value is WorkspaceGroupKey key && key.Id == Id;
    public override int GetHashCode() => StringComparer.Ordinal.GetHashCode(Id);
}
public sealed record ExecutionChoice(string Id, string Title) { public override string ToString() => Title; }

public sealed class ExecutionObject : INotifyPropertyChanged
{
    private bool _recentlyActive;
    public event PropertyChangedEventHandler? PropertyChanged;
    public DateTimeOffset? LastToolCallAt { get; set; }
    public DateTimeOffset? LastActivityAt { get; set; }
    public DateTimeOffset? SortActivityAt { get; set; }
    public bool IsGroupFooter { get; set; }
    public bool HasMore { get; set; }
    public bool AutoLoadMore { get; set; }
    public bool InsertionEligible { get; set; }
    public void Apply(ExecutionObject item)
    {
        if (Id != item.Id || Kind != item.Kind) throw new InvalidOperationException("Row identity changed.");
        Title = item.Title; Detail = item.Detail; Tags = item.Tags; WorkspaceId = item.WorkspaceId;
        ManagementDates = item.ManagementDates; Pinned = item.Pinned; Archived = item.Archived;
        Trashed = item.Trashed; Terminated = item.Terminated; IsUnknown = item.IsUnknown; IsOrphan = item.IsOrphan;
        PendingCount = item.PendingCount; RunningCount = item.RunningCount; Snapshot = item.Snapshot;
        LastToolCallAt = item.LastToolCallAt; LastActivityAt = item.LastActivityAt; SortActivityAt = item.SortActivityAt;
        IsGroupFooter = item.IsGroupFooter; HasMore = item.HasMore; AutoLoadMore = item.AutoLoadMore;
        PropertyChanged?.Invoke(this, new(null));
    }
    public bool RecentlyActive
    {
        get => _recentlyActive;
        set { if (_recentlyActive == value) return; _recentlyActive = value; PropertyChanged?.Invoke(this, new(nameof(RecentlyActive))); }
    }
    public string Id { get; set; } = "";
    public string Kind { get; set; } = "conversation";
    public string Title { get; set; } = "";
    public string Detail { get; set; } = "";
    public string Tags { get; set; } = "";
    public string WorkspaceId { get; set; } = "";
    public WorkspaceGroupKey WorkspaceKey { get; set; } = new("", UiText.Get("ExecutionUnassignedWorkspace"));
    public string ManagementDates { get; set; } = "";
    public bool Pinned { get; set; }
    public bool Archived { get; set; }
    public bool Trashed { get; set; }
    public bool Terminated { get; set; }
    public bool IsUnknown { get; set; }
    public bool IsOrphan { get; set; }
    public long PendingCount { get; set; }
    public long RunningCount { get; set; }
    public JsonElement Snapshot { get; set; }
    public string SelectionKey => IsUnknown ? "unattributed" : Id;
    public static ExecutionObject From(JsonElement value, string kind)
    {
        var title = value.Flag("is_unattributed") ? UiText.Get("ExecutionUnidentifiedConversation") : value.Text("title");
        var created = DateTimeOffset.TryParse(value.Text("created_at"), out var date) ? date.ToLocalTime().ToString("MM-dd HH:mm") : UiText.Get("ExecutionHistory");
        // A user's title is data, even when it happens to equal the old default.
        // The backend's stable title_source identifies product-generated titles.
        if (string.IsNullOrWhiteSpace(title) || (kind == "conversation" && value.Text("title_source") == "fallback")) title = UiText.Format("ExecutionConversationTimestamp", created);
        var workspace = value.Field("state").Text("workspace_id", value.Text("workspace_id"));
        var workspaces = value.Array("workspace_ids");
        if (workspace.Length == 0 && workspaces.Length > 0 && workspaces[0].ValueKind == JsonValueKind.String) workspace = workspaces[0].GetString() ?? "";
        var stats = value.Field("statistics");
        return new ExecutionObject
        {
            Id = value.Text(kind == "task" ? "task_id" : "conversation_id", value.Text("id")), Kind = kind, Title = title,
            WorkspaceId = workspace, Tags = string.Join("、", value.Array("tags").Select(tag => tag.GetString())),
            Detail = kind == "task" ? ExecutionJson.State(value.Text("status")) : value.Text("source"),
            Pinned = value.Flag("pinned"), Archived = value.HasDate("archived_at"), Trashed = value.HasDate("trashed_at"), Terminated = value.HasDate("terminated_at"),
            ManagementDates = created, IsUnknown = value.Flag("is_unattributed"), IsOrphan = value.Flag("is_orphan"),
            PendingCount = stats.Number("pending"), RunningCount = stats.Number("running"), Snapshot = value.Clone(), LastToolCallAt = stats.Date("last_tool_call_at"), LastActivityAt = stats.Date("last_activity_at") ?? stats.Date("last_tool_call_at"), SortActivityAt = value.Date("last_activity_at") ?? stats.Date("last_activity_at") ?? value.Date("created_at")
        };
    }
}

public sealed class ExecutionCallRow : INotifyPropertyChanged
{
    private JsonElement _value;
    private string _output = "";
    private bool _expanded;
    private string _sourceTitle = "";
    private string _sourceState = "unavailable";
    public event PropertyChangedEventHandler? PropertyChanged;
    public ObservableCollection<ExecutionCallRow> Children { get; } = [];
    public string Id => _value.Text("call_id");
    public long CreatedSeq => _value.Number("created_seq");
    public long UpdatedSeq => _value.Number("updated_seq");
    public string Status => _value.Text("status");
    public string State => ExecutionJson.State(Status);
    public string StatusGlyph => Status switch { "succeeded" => "✓", "failed" => "×", "partial" or "unknown" => "!", "pending_approval" => "?", "cancelled" => "–", _ => "…" };
    public string Tool => _value.Text("tool_name");
    public string ApprovalId => _value.Text("approval_id");
    public string ConversationId => _value.Text("conversation_id");
    public string TaskId => _value.Text("task_id");
    public string Command => _value.Text("display_command");
    public string Workdir => _value.Text("workdir");
    public string Parameters => _value.Text("parameter_summary");
    public bool ReadOnlyLegacy => _value.Flag("read_only_legacy");
    public string Summary => _value.Text("summary");
    public string Title
    {
        get
        {
            // Never translate user text by comparing it with an old default.
            // History without trustworthy provenance remains verbatim.
            var title = _value.Text("activity_label", _value.Text("display_title", _value.Text("title", Tool)));
            if (_value.Text("activity_label_source") == "tool")
            {
                var key = "ExecutionTool_" + Tool;
                var localized = UiText.Get(key);
                title = localized == key ? Tool : localized;
            }
            return title.Replace('\r', ' ').Replace('\n', ' ');
        }
    }
    public DateTimeOffset? RequestReceivedAt => _value.Date("request_received_at");
    public DateTimeOffset? LastActivityAt => _value.Date("last_activity_at");
    public long? RpcElapsedMs => _value.OptionalNumber("rpc_elapsed_ms");
    public string Duration => FormatDuration(RpcElapsedMs ?? (_value.Number("elapsed_ms") > 0 ? _value.Number("elapsed_ms") : null));
    public string ExecutionDuration => FormatDuration(_value.OptionalNumber("execution_elapsed_ms"));
    public string WaitDuration => FormatDuration(_value.OptionalNumber("wait_elapsed_ms"));
    public string ActualTool => Tool == "file_edit" ? "file_edit · EDIT_FILE" : Tool;
    public string Started => _value.Date("started_at")?.ToLocalTime().ToString("HH:mm:ss.fff") ?? When;
    public string SourceType => _value.Text("source", UiText.Get("ExecutionNotRecorded"));
    public string TimingDetails => string.Join("\n", new[]
    {
        UiText.Format("ExecutionToolValue", Tool),
        UiText.Format("ExecutionRpcReturnedValue", _value.Date("rpc_completed_at")?.ToLocalTime().ToString("yyyy-MM-dd HH:mm:ss.fff") ?? UiText.Get("ExecutionNotRecorded")),
        UiText.Format("ExecutionRpcDurationValue", FormatDuration(RpcElapsedMs)),
        UiText.Format("ExecutionStageDurationValue", ExecutionDuration),
        UiText.Format("ExecutionWaitDurationValue", WaitDuration),
        UiText.Format("ExecutionOperationDurationValue", FormatDuration(_value.OptionalNumber("operation_elapsed_ms"))),
        UiText.Format("ExecutionProcessDurationValue", FormatDuration(_value.OptionalNumber("process_elapsed_ms"))),
        UiText.Get("ExecutionConcurrentTimingNotice")
    });
    public string FileEditDetails
    {
        get
        {
            var edit = _value.Field("file_edit");
            if (edit.ValueKind != JsonValueKind.Object) return UiText.Get("ExecutionFileDetailsNotRecorded");
            var changed = edit.Field("changed").ValueKind switch { JsonValueKind.True => UiText.Get("ExecutionYes"), JsonValueKind.False => UiText.Get("ExecutionNo"), _ => UiText.Get("ExecutionResultUnknown") };
            var files = edit.Array("affected_files").Select(file => file.Text("path") + (file.Text("move_to").Length > 0 ? " → " + file.Text("move_to") : ""));
            return UiText.Format("ExecutionFileEditSummary", edit.Text("action"), edit.Text("path"),
                edit.Flag("dry_run") ? UiText.Get("ExecutionPreviewNotWritten") : UiText.Get("ExecutionNo"),
                edit.Flag("executed") ? UiText.Get("ExecutionYes") : UiText.Get("ExecutionNo"), changed,
                edit.OptionalNumber("affected_count")?.ToString() ?? UiText.Get("ExecutionNotRecorded"),
                edit.OptionalNumber("insertions")?.ToString() ?? UiText.Get("ExecutionNotRecorded"),
                edit.OptionalNumber("deletions")?.ToString() ?? UiText.Get("ExecutionNotRecorded"))
                + string.Join("\n", files) + (edit.Flag("files_truncated") ? "\n" + UiText.Get("ExecutionFileListTruncated") : "")
                + "\n\n" + edit.Text("diff_preview") + (edit.Flag("diff_truncated") ? "\n" + UiText.Get("ExecutionDiffTruncated") : "");
        }
    }
    private static string FormatDuration(long? milliseconds) => milliseconds is >= 0 ? (milliseconds.Value / 1000.0).ToString("0.000") + " s" : UiText.Get("ExecutionNotRecorded");
    public string When => DateTimeOffset.TryParse(_value.Text("created_at"), out var date) ? date.ToLocalTime().ToString("HH:mm:ss") : "";
    public string Rule => string.Join(" · ", new[] { _value.Text("rule_id"), ExecutionJson.Mode(_value.Text("permission_mode")) }.Where(value => value.Length > 0));
    public string SourceState => _sourceState;
    public string Origin => ConversationId.Length == 0 ? UiText.Get("ExecutionUnassigned") : _sourceState switch
    {
        "resolved" => _sourceTitle, "loading" => UiText.Get("ExecutionSourceLoading"), "deleted" => UiText.Get("ExecutionSourceDeleted"), "error" => UiText.Get("ExecutionSourceFailed"), _ => UiText.Get("ExecutionSourceUnavailable")
    };
    public string SourceTitle { get => _sourceTitle; set => SetSource(value, "resolved"); }
    public void SetSource(string title, string state) { _sourceTitle = title; _sourceState = state; Notify(); }
    public bool CanRetry => !ReadOnlyLegacy && Status is "failed" or "cancelled";
    public bool CanStop => !ReadOnlyLegacy && Status is "created" or "running" or "pending_approval";
    public bool NeedsApproval => Status == "pending_approval";
    public bool NeedsVerification => Status == "unknown";
    public bool HasChanges => _value.Array("file_changes").Length > 0;
    public string Changes => string.Join("\n", _value.Array("file_changes").Select(change => change.Text("path") + (change.Flag("stats_known") ? $"  +{change.Number("insertions")} −{change.Number("deletions")}" : "")));
    public string Technical => _value.Pretty();
    public bool IsExpanded { get => _expanded; set { _expanded = value; Notify(); } }
    public bool FollowOutput { get; set; } = true;
    public bool DetailLoaded { get; private set; }
    public string Output => _output;
    public string HistoryWarning => _value.Flag("history_incomplete") ? UiText.Get("ExecutionHistoryIncomplete") : "";
    public ExecutionCallRow(JsonElement value) { _value = value.Clone(); }
    public bool VisibleIn(string view)
    {
        if (_value.HasDate("deleted_at")) return false;
        return view switch
        {
            "all" => true,
            "trash" => _value.HasDate("trashed_at"),
            "archived" => !_value.HasDate("trashed_at") && _value.HasDate("archived_at"),
            "isolated" => !_value.HasDate("trashed_at") && _value.HasDate("isolated_at"),
            _ => !_value.HasDate("trashed_at") && !_value.HasDate("archived_at") && !_value.HasDate("isolated_at")
        };
    }
    public void Apply(JsonElement value) { if (value.Number("updated_seq") < UpdatedSeq) return; _value = value.Clone(); Notify(); }
    public void ApplyDetail(JsonElement value)
    {
        if (value.Number("updated_seq") < UpdatedSeq) return;
        Apply(value); DetailLoaded = true;
        var output = value.Text("output_preview"); var error = value.Text("stderr_preview");
        if (error.Length > 0) output += (output.Length > 0 ? "\n\n" : "") + UiText.Get("ExecutionStandardError") + "\n" + error;
        if (output.Length == 0) output = value.Text("summary", UiText.Get("ExecutionNoOutput"));
        if (value.Flag("stdout_truncated") || value.Flag("stderr_truncated")) output = UiText.Get("ExecutionOutputTruncated") + "\n\n" + output;
        _output = output; Notify();
    }
    private void Notify() => PropertyChanged?.Invoke(this, new PropertyChangedEventArgs(null));
}

public sealed class ExecutionPreferences
{
    public int SchemaVersion { get; set; } = 3;
    public int RetentionDays { get; set; } = 30;
    public double FontSize { get; set; } = 14;
    public bool Notifications { get; set; } = true;
    public string LastView { get; set; } = "conversation";
    public string LastKind { get; set; } = "conversation";
    public string Theme { get; set; } = "system";
    public bool DetailedCalls { get; set; }
    public string LastConversation { get; set; } = "";
    public HashSet<string> CollapsedWorkspaces { get; set; } = [];
    public Dictionary<string, string[]> SavedFilters { get; set; } = [];
    public HashSet<string> DismissedNotices { get; set; } = [];
    [System.Text.Json.Serialization.JsonExtensionData]
    public Dictionary<string, JsonElement> AdditionalPreferences { get; set; } = [];
}
