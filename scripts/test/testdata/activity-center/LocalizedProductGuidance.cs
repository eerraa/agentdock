using System.Globalization;
using System.IO;
using System.Reflection;
using System.Text.Json;
using System.Windows;
using System.Windows.Controls;
using AgentDock.ControlPanel;

internal static partial class Program
{
    private static void TestLocalizedProductGuidance(string root)
    {
        Directory.CreateDirectory(root);
        var originalLocale = ((CultureInfo)typeof(UiText).GetField("_resourceCulture", BindingFlags.NonPublic | BindingFlags.Static)!.GetValue(null)!).Name;
        var originalCulture = CultureInfo.CurrentCulture;
        var originalThemePath = (string)typeof(DesktopTheme).GetField("_path", BindingFlags.NonPublic | BindingFlags.Static)!.GetValue(null)!;
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
                var window = new MainWindow(runtime);
                try
                {
                    var status = new NativeTunnelStatus
                    {
                        Phase = "NeedsApproval", DiagnosticCode = "funnel_permission_required",
                        Diagnostic = "fixture original diagnosis 中文 한국어", PublicUrl = "https://fixture.example",
                        BinaryPath = @"D:\한글 작업 공간\tailscale.exe"
                    };
                    typeof(MainWindow).GetMethod("ApplyTailscaleStatus", BindingFlags.NonPublic | BindingFlags.Instance)!.Invoke(window, [status]);
                    Require(((TextBlock)window.FindName("TailscaleFunnelText")).Text == UiText.Get("FunnelNeedsApproval"), "The actual main-window tunnel phase missed its locale.");
                    var diagnostic = (TextBlock)window.FindName("TailscaleDiagnosticText");
                    Require(diagnostic.Text == UiText.Get("TunnelDiagnostic_funnel_permission_required"), "The main-window diagnosis did not use its stable code.");
                    Require(diagnostic.ToolTip?.ToString() == status.OriginalDiagnostic && status.OriginalDiagnostic.Contains(status.Diagnostic), "Original product diagnostics were lost or mislabeled.");
                    Require(((Button)window.FindName("TailscaleAuthorizeButton")).IsEnabled, "Localization disabled the authorized permission action.");
                    status.DiagnosticCode = "fixture_future_code";
                    typeof(MainWindow).GetMethod("ApplyTailscaleStatus", BindingFlags.NonPublic | BindingFlags.Instance)!.Invoke(window, [status]);
                    Require(diagnostic.Text == status.Diagnostic && !((Button)window.FindName("TailscaleAuthorizeButton")).IsEnabled, "Unknown diagnostics were rewritten or changed authorization logic.");
                    var nullDiagnostic = JsonSerializer.Deserialize<NativeTunnelStatus>("{\"diagnostic_code\":null,\"diagnostic\":null}")!;
                    Require(nullDiagnostic.DisplayDiagnostic == "" && nullDiagnostic.OriginalDiagnostic == "", "A nullable diagnostic caused a presentation failure.");
                    var wire = JsonSerializer.Serialize(status);
                    Require(!wire.Contains("DisplayDiagnostic") && !wire.Contains("OriginalDiagnostic"), "Display-only properties leaked into protocol data.");
                    var parse = typeof(DisplayPreferenceService).GetMethod("Parse", BindingFlags.NonPublic | BindingFlags.Static)!;
                    var response = JsonSerializer.SerializeToElement(new
                    {
                        chatgpt_mcp_ui_enabled = false, revision = 7,
                        warning = "legacy owned warning", warning_code = "display_preferences_load_failed", warning_detail = "fixture I/O detail",
                        refresh_hint = "legacy owned guidance", refresh_hint_code = "refresh_chatgpt_connection"
                    });
                    var preference = (McpUiPreference)parse.Invoke(null, [response])!;
                    Require(!preference.Enabled && preference.Revision == 7, "Localized presentation changed preference data.");
                    Require(preference.Warning == UiText.Format("ThemeLoadWarning", "fixture I/O detail") && preference.RefreshHint == UiText.Get("DisplayRefreshConnectionHint"), "Structured backend guidance was not localized.");
                    var legacy = JsonSerializer.SerializeToElement(new { chatgpt_mcp_ui_enabled = true, revision = 9, warning = "unknown original warning", refresh_hint = "unknown original hint" });
                    var legacyPreference = (McpUiPreference)parse.Invoke(null, [legacy])!;
                    Require(legacyPreference.Warning == "unknown original warning" && legacyPreference.RefreshHint == "unknown original hint", "Compatibility fallback rewrote unknown server text.");
                    try { parse.Invoke(null, [JsonSerializer.SerializeToElement(new { revision = 1 })]); throw new InvalidOperationException("Invalid preferences were accepted."); }
                    catch (TargetInvocationException error) when (error.InnerException is JsonException detail)
                    { Require(detail.Message == UiText.Get("DisplayPreferenceInvalid"), "Invalid display response did not produce localized guidance."); }
                    var refresh = (Task)typeof(MainWindow).GetMethod("RefreshActivitySummaryAsync", BindingFlags.NonPublic | BindingFlags.Instance)!.Invoke(window, null)!;
                    PumpUntil(() => refresh.IsCompleted, TimeSpan.FromSeconds(8)); refresh.GetAwaiter().GetResult();
                    var summary = ((TextBlock)window.FindName("ActivitySummaryText")).Text;
                    Require(!summary.Contains("运行中") || language == "zh-CN", "Activity summary leaked Chinese into another locale.");
                    Require(!summary.StartsWith(UiText.Get("StatusUnavailable")), "Activity-summary acceptance used a failed request instead of loaded data.");
                    metrics.Add(new { language, loaded_tunnel_phase = true, stable_diagnosis_and_permission = true, loaded_activity_summary = summary, warning_code_and_original_detail = true });
                }
                finally { window.CloseForReplacement(); }
                Require(fixture.ControlCount == 0, "Viewing localized product guidance mutated task state.");
            }
            var corrupt = Path.Combine(root, "corrupt-theme"); Directory.CreateDirectory(corrupt);
            var path = Path.Combine(corrupt, "execution-center-settings.json");
            const string invalid = "invalid original settings"; File.WriteAllText(path, invalid);
            ApplyTestUiLanguage("ko-KR"); DesktopTheme.Initialize(corrupt);
            Require(DesktopTheme.LoadWarning.StartsWith("표시 설정을 불러오지 못했으며"), "Corrupt theme warning was not localized.");
            ApplyTestUiLanguage("en");
            Require(DesktopTheme.LoadWarning.StartsWith("Display settings could not be loaded"), "A cached warning retained the previous UI language.");
            Require(File.ReadAllText(path) == invalid, "Localization rewrote corrupt preferences.");
            try { DesktopTheme.Save("fixture-invalid"); throw new InvalidOperationException("An invalid theme was accepted."); }
            catch (ArgumentException error) { Require(error.Message == UiText.Get("ThemeSelectionInvalid"), "Theme validation missed the selected locale."); }
            Require(CultureInfo.CurrentCulture.Name == originalCulture.Name, "Guidance localization changed numeric/date culture.");
        }
        finally
        {
            ApplyTestUiLanguage(originalLocale);
            if (originalThemePath.Length > 0) DesktopTheme.Initialize(Path.GetDirectoryName(originalThemePath)!);
        }
        File.WriteAllText(Path.Combine(root, "product-guidance-result.json"), JsonSerializer.Serialize(new { passed = true, metrics, source = "isolated HTTP and loaded WPF controls, not installed-package or actual OS DPI acceptance" }, new JsonSerializerOptions { WriteIndented = true }));
    }
}
