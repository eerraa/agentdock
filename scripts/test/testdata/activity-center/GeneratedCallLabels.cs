using System.Globalization;
using System.Text.Json;
using AgentDock.ControlPanel;

internal static partial class Program
{
    private static void TestGeneratedCallLabels()
    {
        foreach (var language in new[] { "ko-KR", "en", "zh-CN" })
        {
            ApplyTestUiLanguage(language);
            var expected = language switch { "ko-KR" => "텍스트 검색", "zh-CN" => "搜索文本", _ => "Search text" };
            var generated = new ExecutionCallRow(JsonSerializer.SerializeToElement(new
            {
                call_id = "generated", tool_name = "search_text", activity_label = "Search text",
                activity_label_source = "tool", status = "succeeded", updated_seq = 1,
                display_command = "literal original 中文 한국어"
            }));
            Require(generated.Title == expected, "Product-generated call label missed its selected language: " + language);
            Require(generated.Tool == "search_text" && generated.Command == "literal original 中文 한국어"
                && generated.Technical.Contains("Search text"), "Localization changed technical identities or raw stored text.");
            foreach (var source in new[] { "", "user", "future-source" })
            {
                var explicitLabel = new ExecutionCallRow(JsonSerializer.SerializeToElement(new
                { tool_name = "search_text", activity_label = "Search text", activity_label_source = source }));
                Require(explicitLabel.Title == "Search text", "An explicit or unclassified historical label was translated.");
            }
            var legacy = new ExecutionCallRow(JsonSerializer.SerializeToElement(new
            { tool_name = "search_text", activity_label = "用户 지정 한국어", display_title = "Search text" }));
            Require(legacy.Title == "用户 지정 한국어", "Legacy user text was rewritten by a title heuristic.");
            var future = new ExecutionCallRow(JsonSerializer.SerializeToElement(new
            { tool_name = "future_tool", activity_label = "Future", activity_label_source = "tool" }));
            Require(future.Title == "future_tool", "Unknown generated tools must retain their canonical identifier.");
            generated.Apply(JsonSerializer.SerializeToElement(new
            { call_id = "generated", tool_name = "search_text", activity_label = "사용자 제목", updated_seq = 2 }));
            Require(generated.Title == "사용자 제목", "An incremental explicit label retained stale generated provenance.");
        }
        ApplyTestUiLanguage("ko-KR");
    }
}
