using System.Windows.Markup;

namespace AgentDock.ControlPanel;

internal static class ActivityText
{
    internal static string Get(string key) => UiText.Get("Activity_" + key);

    internal static string State(string value)
    {
        var key = value switch
        {
            "active" => "State_active",
            "blocked" => "State_blocked",
            "completed" => "State_completed",
            "cancelled" => "State_cancelled",
            "archived" => "State_archived",
            "open" => "State_open",
            "closed" => "State_closed",
            "success" or "pass" => "State_success",
            "failed" => "State_failed",
            "running" => "State_running",
            "timeout" => "State_timeout",
            "killed" => "State_killed",
            "pending" => "State_pending",
            "in_progress" => "State_in_progress",
            "partial" => "State_partial",
            "exited" => "State_exited",
            _ => ""
        };
        return key.Length == 0 ? value : Get(key);
    }

    internal static string Kind(string value)
    {
        var key = value switch
        {
            "command.started" or "command.output" or "command.completed" => "Kind_command",
            "task.created" => "Kind_task_created",
            "task.completed" => "Kind_task_completed",
            "task.blocked" => "Kind_task_blocked",
            "task.resumed" => "Kind_task_resumed",
            "task.archived" => "Kind_task_archived",
            "task.unarchived" => "Kind_task_unarchived",
            "thread.created" => "Kind_thread_created",
            "thread.switched" => "Kind_thread_switched",
            "thread.blocked" => "Kind_thread_blocked",
            "thread.resumed" => "Kind_thread_resumed",
            "thread.closed" => "Kind_thread_closed",
            "activity.cleaned" => "Kind_activity_cleaned",
            "file.changed" => "Kind_file_changed",
            "step.started" => "Kind_step_started",
            "step.summary" => "Kind_step_summary",
            "tool.started" or "tool.completed" => "Kind_tool",
            "review.completed" => "Kind_review_completed",
            _ => ""
        };
        return key.Length == 0 ? value : Get(key);
    }
}

[MarkupExtensionReturnType(typeof(string))]
internal sealed class ActivityLocExtension(string key) : MarkupExtension
{
    [ConstructorArgument("key")]
    public string Key { get; set; } = key;
    public override object ProvideValue(IServiceProvider serviceProvider) => ActivityText.Get(Key);
}
