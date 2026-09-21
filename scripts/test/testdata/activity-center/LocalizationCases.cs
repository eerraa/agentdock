using System.Globalization;
using System.Resources;
using AgentDock.ControlPanel;

internal static partial class Program
{
    private static void TestLocalization()
    {
        Require(UiText.NormalizePreference(" en ") == UiText.EnglishPreference, "English preference was not normalized.");
        Require(UiText.NormalizePreference("ZH-cn") == UiText.SimplifiedChinesePreference, "Chinese preference was not normalized.");
        Require(UiText.NormalizePreference(" KO-kr ") == UiText.KoreanPreference, "Korean preference was not normalized.");
        Require(UiText.NormalizePreference("") == UiText.SystemPreference && UiText.NormalizePreference("fr-FR") == UiText.SystemPreference, "Unknown preferences must stay on the system choice.");
        Require(UiText.ResolveLocale(UiText.SystemPreference, "zh-Hans-CN") == UiText.SimplifiedChinesePreference, "Simplified Chinese system locales must resolve together.");
        Require(UiText.ResolveLocale(UiText.SystemPreference, "ko") == UiText.KoreanPreference, "The bare ko tag must resolve to ko-KR.");
        Require(UiText.ResolveLocale(UiText.SystemPreference, "ko-KR") == UiText.KoreanPreference, "ko-KR must resolve to Korean.");
        Require(UiText.ResolveLocale(UiText.SystemPreference, "ko-KP") == UiText.EnglishPreference, "Other Korean region tags must stay English.");
        Require(UiText.ResolveLocale(UiText.KoreanPreference, "en-US") == UiText.KoreanPreference, "An explicit preference must win over the system locale.");

        var formattingCulture = CultureInfo.CurrentCulture;
        try
        {
            UiText.ApplyResourceCultureForTests("en");
            CultureInfo.CurrentCulture = CultureInfo.GetCultureInfo("en-US");
            Require(UiText.Get("Overview") == "Overview", "English resources did not load.");
            Require(ActivityText.Get("NeedsAttention") == "Needs attention", "Activity text left the shared resource manager.");
            Require(ExecutionText.Get("GlyphPendingApproval") == "A", "English pending glyph changed.");
            Require(UiText.Format("Execution_DurationSeconds", 1.5) == "1.500 s", "Formatting ignored the current culture.");

            UiText.ApplyResourceCultureForTests("zh-CN");
            Require(ActivityText.State("pass") == "成功" && ActivityText.Kind("command.completed") == "命令", "Known activity states did not localize.");
            Require(ActivityText.State("not-a-state") == "not-a-state", "Unknown activity state was rewritten.");
            Require(ExecutionText.Get("GlyphPendingApproval") == "审", "Chinese pending glyph changed.");

            UiText.ApplyResourceCultureForTests("ko-KR");
            CultureInfo.CurrentCulture = CultureInfo.GetCultureInfo("en-US");
            Require(UiText.Format("Execution_DurationSeconds", 1.5) == "1.500 s", "Korean UI culture changed numeric formatting.");
            Require(ExecutionText.Get("GlyphPendingApproval") == "승", "Korean pending glyph changed.");
            Require(ActivityText.Get("NeedsAttention") != "Needs attention" && ActivityText.Get("NeedsAttention") != "待处理任务", "Korean activity text did not load independently.");
            Require(UiText.Get("ThisKeyDoesNotExistAnywhere") == "ThisKeyDoesNotExistAnywhere", "A missing resource must return its key.");
        }
        finally
        {
            CultureInfo.CurrentCulture = formattingCulture;
            UiText.ApplyResourceCultureForTests("zh-CN");
        }

        Require(!ExecutionJson.IsLegacyPlaceholderTitle("사용자 제목", "manual"), "A manual title was treated as a placeholder.");
        Require(!ExecutionJson.IsLegacyPlaceholderTitle("新对话", "host"), "A host title was rewritten.");
        Require(!ExecutionJson.IsLegacyPlaceholderTitle("", "task"), "An empty task-sourced title was rewritten.");
        Require(!ExecutionJson.IsLegacyPlaceholderTitle("작업", "operation"), "An operation title was rewritten.");
        Require(ExecutionJson.IsLegacyPlaceholderTitle("新对话", ""), "The historical placeholder was not recognized.");
        Require(ExecutionJson.IsLegacyPlaceholderTitle("  ", "fallback"), "An empty legacy title was kept.");

        var resources = new ResourceManager("ActivityCenterTests.Resources.FallbackStrings", typeof(LocalizationAnchor).Assembly);
        var korean = CultureInfo.GetCultureInfo("ko-KR");
        Require(resources.GetString("Hello", korean) == "안녕", "The Korean satellite did not load.");
        Require(resources.GetString("OnlyNeutral", korean) == "neutral-english", "A missing satellite key did not fall back to neutral English.");
        Require(resources.GetString("MissingEverywhere", korean) is null, "ResourceManager invented a value for a missing key.");
    }
}

internal static class LocalizationAnchor;
