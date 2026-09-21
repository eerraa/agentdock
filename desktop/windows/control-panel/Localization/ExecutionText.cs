namespace AgentDock.ControlPanel;

internal static class ExecutionText
{
    internal static string Get(string key) => UiText.Get("Execution_" + key);
    internal static string Format(string key, params object?[] args) => UiText.Format("Execution_" + key, args);
}
