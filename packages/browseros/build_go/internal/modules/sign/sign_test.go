package sign

import (
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/browseros-ai/BrowserOS/packages/browseros/build_go/internal/buildctx"
	"github.com/browseros-ai/BrowserOS/packages/browseros/build_go/internal/execx"
	"github.com/browseros-ai/BrowserOS/packages/browseros/build_go/internal/platform"
	"github.com/browseros-ai/BrowserOS/packages/browseros/build_go/internal/sparkle"
)

var (
	macArm = platform.Platform{OS: "macos", Arch: "arm64"}
	winX64 = platform.Platform{OS: "windows", Arch: "x64"}
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeExec(t *testing.T, path string) {
	t.Helper()
	writeFile(t, path, "#!/bin/sh\n")
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func fixtureCtx(t *testing.T, plat platform.Platform) (*buildctx.Context, *execx.RecordingRunner) {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "pyproject.toml"), "name = \"browseros\"\n")
	writeFile(t, filepath.Join(root, "CHROMIUM_VERSION"), "MAJOR=148\nMINOR=0\nBUILD=7778\nPATCH=97\n")
	writeFile(t, filepath.Join(root, "build", "config", "BROWSEROS_BUILD_OFFSET"), "162\n")
	writeFile(t, filepath.Join(root, "resources", "BROWSEROS_VERSION"), "BROWSEROS_MAJOR=0\nBROWSEROS_MINOR=46\nBROWSEROS_BUILD=17\nBROWSEROS_PATCH=0\n")

	src := filepath.Join(t.TempDir(), "src")
	os.MkdirAll(src, 0o755)

	rec := &execx.RecordingRunner{}
	ctx, err := buildctx.New(buildctx.Options{
		ChromiumSrc: src, Architecture: plat.Arch, BuildType: "release",
		Platform: &plat, RootDir: root, Runner: rec,
	})
	if err != nil {
		t.Fatal(err)
	}
	return ctx, rec
}

// buildFixtureApp stages a minimal BrowserOS.app bundle with one of each
// component category.
func buildFixtureApp(t *testing.T, ctx *buildctx.Context) string {
	t.Helper()
	app := ctx.AppPath()
	fw := filepath.Join(app, "Contents", "Frameworks")
	browserFW := filepath.Join(fw, "BrowserOS Framework.framework")
	versioned := filepath.Join(browserFW, "Versions", ctx.BrowserOSChromiumVersion)

	writeExec(t, filepath.Join(app, "Contents", "MacOS", "BrowserOS"))
	writeExec(t, filepath.Join(versioned, "Helpers", "BrowserOS Helper (Renderer).app", "Contents", "MacOS", "helper"))
	writeExec(t, filepath.Join(versioned, "Helpers", "chrome_crashpad_handler"))
	writeFile(t, filepath.Join(versioned, "Libraries", "libEGL.dylib"), "dylib")
	writeFile(t, filepath.Join(fw, "Sparkle.framework", "Modules", "module"), "x")
	writeExec(t, filepath.Join(fw, "Sparkle.framework", "Versions", "B", "Autoupdate"))
	writeFile(t, filepath.Join(fw, "Sparkle.framework", "XPCServices", "Downloader.xpc", "Contents", "Info.plist"), "<plist/>")
	writeExec(t, filepath.Join(app, "Contents", "Resources", "BrowserOSServer", "default", "resources", "bin", "browseros_server"))
	writeExec(t, filepath.Join(app, "Contents", "Resources", "BrowserOSServer", "default", "resources", "bin", "codex"))
	return app
}

func TestFindComponentsToSignDiscoversAllCategories(t *testing.T) {
	ctx, _ := fixtureCtx(t, macArm)
	app := buildFixtureApp(t, ctx)

	c := FindComponentsToSign(ctx, app)
	if len(c.Helpers) != 1 || !strings.Contains(c.Helpers[0], "Helper (Renderer).app") {
		t.Errorf("helpers = %v", c.Helpers)
	}
	if len(c.XPCServices) != 1 || !strings.Contains(c.XPCServices[0], "Downloader.xpc") {
		t.Errorf("xpc = %v", c.XPCServices)
	}
	var execNames []string
	for _, exe := range c.Executables {
		execNames = append(execNames, filepath.Base(exe))
	}
	joined := strings.Join(execNames, ",")
	for _, want := range []string{"chrome_crashpad_handler", "Autoupdate", "browseros_server", "codex"} {
		if !strings.Contains(joined, want) {
			t.Errorf("executables missing %s: %v", want, execNames)
		}
	}
	if len(c.Dylibs) != 1 || !strings.Contains(c.Dylibs[0], "libEGL.dylib") {
		t.Errorf("dylibs = %v", c.Dylibs)
	}
	if len(c.Frameworks) < 2 {
		t.Errorf("frameworks = %v", c.Frameworks)
	}
}

func TestIdentifierForComponent(t *testing.T) {
	cases := map[string]string{
		"/x/Sparkle.framework/Versions/B/Autoupdate":      "org.sparkle-project.Autoupdate",
		"/x/Helpers/chrome_crashpad_handler":              "com.browseros.crashpad_handler",
		"/x/Helpers/BrowserOS Helper (Renderer).app":      "com.browseros.helper.renderer",
		"/x/Helpers/BrowserOS Helper (GPU).app":           "com.browseros.helper.gpu",
		"/x/Frameworks/BrowserOS Framework.framework":     "com.browseros.framework",
		"/x/Libraries/libEGL.dylib":                       "com.browseros.libEGL",
		"/x/BrowserOSServer/default/resources/bin/codex":  "com.browseros.codex",
		"/x/BrowserOSServer/default/resources/bin/claude": "com.browseros.claude",
	}
	for path, want := range cases {
		if got := IdentifierForComponent(path); got != want {
			t.Errorf("IdentifierForComponent(%s) = %q, want %q", path, got, want)
		}
	}
}

func TestSigningOptions(t *testing.T) {
	cases := map[string]string{
		"/x/Sparkle.framework/XPCServices/Downloader.xpc": "runtime",
		"/x/Helpers/BrowserOS Helper (Renderer).app":      "restrict,kill,runtime",
		"/x/Libraries/libEGL.dylib":                       "restrict,library,runtime,kill",
		"/x/bin/browseros_server":                         "runtime",
		"/x/Contents/MacOS/SomethingElse":                 "runtime",
	}
	for path, want := range cases {
		if got := SigningOptions(path); got != want {
			t.Errorf("SigningOptions(%s) = %q, want %q", path, got, want)
		}
	}
}

func TestSignAllComponentsOrderAndArgs(t *testing.T) {
	ctx, rec := fixtureCtx(t, macArm)
	app := buildFixtureApp(t, ctx)
	writeFile(t, filepath.Join(ctx.EntitlementsDir(), "browseros-executable-entitlements.plist"), "<plist/>")
	writeFile(t, filepath.Join(ctx.EntitlementsDir(), "helper-renderer-entitlements.plist"), "<plist/>")
	writeFile(t, filepath.Join(ctx.EntitlementsDir(), "app-entitlements.plist"), "<plist/>")

	if err := SignAllComponents(ctx, app, "Developer ID Application: Test"); err != nil {
		t.Fatal(err)
	}

	argv := rec.Argv()
	for _, cmd := range argv {
		if !strings.HasPrefix(cmd, "codesign --sign Developer ID Application: Test --force --timestamp") {
			t.Errorf("unexpected command: %q", cmd)
		}
	}

	find := func(substr string) int {
		for i, cmd := range argv {
			if strings.Contains(cmd, substr) {
				return i
			}
		}
		t.Fatalf("no command containing %q in:\n%s", substr, strings.Join(argv, "\n"))
		return -1
	}

	// Bottom-up order: XPC before frameworks; Sparkle.framework before
	// BrowserOS Framework; main exe before the final app bundle.
	xpcIdx := find("Downloader.xpc")
	sparkleFwIdx := find("--identifier org.sparkle-project.Sparkle ")
	mainFwIdx := find("--identifier com.browseros.framework ")
	mainExeIdx := find("Contents/MacOS/BrowserOS")
	bundleIdx := find("--requirements")
	if !(xpcIdx < sparkleFwIdx && sparkleFwIdx < mainFwIdx && mainFwIdx < mainExeIdx && mainExeIdx < bundleIdx) {
		t.Errorf("sign order wrong: xpc=%d sparkleFw=%d mainFw=%d mainExe=%d bundle=%d\n%s",
			xpcIdx, sparkleFwIdx, mainFwIdx, mainExeIdx, bundleIdx, strings.Join(argv, "\n"))
	}

	// Server binary gets its entitlements; renderer helper gets its plist.
	serverIdx := find("bin/browseros_server")
	if !strings.Contains(argv[serverIdx], "browseros-executable-entitlements.plist") {
		t.Errorf("server binary should be signed with entitlements: %q", argv[serverIdx])
	}
	helperIdx := find("Helper (Renderer).app")
	if !strings.Contains(argv[helperIdx], "helper-renderer-entitlements.plist") {
		t.Errorf("renderer helper should use renderer entitlements: %q", argv[helperIdx])
	}
	// Final bundle uses the hardened options + requirements + app entitlements.
	if !strings.Contains(argv[bundleIdx], "--options restrict,library,runtime,kill") ||
		!strings.Contains(argv[bundleIdx], "app-entitlements.plist") {
		t.Errorf("bundle sign command: %q", argv[bundleIdx])
	}
}

func TestNotarizeAppSequence(t *testing.T) {
	ctx, rec := fixtureCtx(t, macArm)
	app := buildFixtureApp(t, ctx)
	t.Setenv("PROD_MACOS_NOTARIZATION_APPLE_ID", "dev@browseros.com")
	t.Setenv("PROD_MACOS_NOTARIZATION_TEAM_ID", "TEAM123")
	t.Setenv("PROD_MACOS_NOTARIZATION_PWD", "secret")

	rec.Handler = func(c execx.Cmd) (execx.Result, error) {
		if strings.Contains(c.String(), "notarytool submit") {
			return execx.Result{Stdout: "id: abc-123\nstatus: Accepted\n"}, nil
		}
		return execx.Result{}, nil
	}

	if err := NotarizeApp(ctx, app); err != nil {
		t.Fatal(err)
	}

	argv := rec.Argv()
	wantPrefixes := []string{
		"ditto -c -k --keepParent",
		"xcrun notarytool store-credentials notarytool-profile",
		"xcrun notarytool submit",
		"xcrun stapler staple",
		"spctl -a -vvv",
		"xcrun stapler validate",
	}
	if len(argv) != len(wantPrefixes) {
		t.Fatalf("got %d commands:\n%s", len(argv), strings.Join(argv, "\n"))
	}
	for i, prefix := range wantPrefixes {
		if !strings.HasPrefix(argv[i], prefix) {
			t.Errorf("cmd[%d] = %q, want prefix %q", i, argv[i], prefix)
		}
	}
	if !strings.Contains(argv[2], "--keychain-profile notarytool-profile --wait") {
		t.Errorf("submit should use keychain profile: %q", argv[2])
	}
}

func TestNotarizeAppFailsWhenNotAccepted(t *testing.T) {
	ctx, rec := fixtureCtx(t, macArm)
	app := buildFixtureApp(t, ctx)
	rec.Handler = func(c execx.Cmd) (execx.Result, error) {
		if strings.Contains(c.String(), "notarytool submit") {
			return execx.Result{Stdout: "id: xyz\nstatus: Invalid\n"}, nil
		}
		return execx.Result{}, nil
	}
	err := NotarizeApp(ctx, app)
	if err == nil || !strings.Contains(err.Error(), "Accepted") {
		t.Errorf("err = %v", err)
	}
}

func TestVerifyServerResourcesBundleDetectsDrift(t *testing.T) {
	ctx, _ := fixtureCtx(t, macArm)
	app := ctx.AppPath()

	// Staged tree has two files (one executable); bundle is missing one and
	// lost the exec bit on the other.
	staged := filepath.Join(ctx.ChromiumSrc, "chrome", "browser", "browseros", "server", "resources")
	writeExec(t, filepath.Join(staged, "bin", "browseros_server"))
	writeFile(t, filepath.Join(staged, "config.json"), "{}")
	writeFile(t, filepath.Join(app, "Contents", "Resources", "BrowserOSServer", "default", "resources", "bin", "browseros_server"), "x")

	problems := VerifyServerResourcesBundle(app, ctx.ChromiumSrc)
	joined := strings.Join(problems, "\n")
	if !strings.Contains(joined, "lost executable bit in app bundle: bin/browseros_server") {
		t.Errorf("missing exec-bit problem: %v", problems)
	}
	if !strings.Contains(joined, "missing from app bundle: config.json") {
		t.Errorf("missing file problem: %v", problems)
	}

	// No staged tree → no problems (sign-only flows).
	os.RemoveAll(staged)
	if problems := VerifyServerResourcesBundle(app, ctx.ChromiumSrc); len(problems) != 0 {
		t.Errorf("problems without staged tree = %v", problems)
	}
}

func TestSparkleSignRoundTrip(t *testing.T) {
	// Generate a key, sign a fixture DMG via the module, verify with the
	// derived public key.
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	key := ed25519.NewKeyFromSeed(seed)
	t.Setenv("SPARKLE_PRIVATE_KEY", base64.StdEncoding.EncodeToString(seed))

	ctx, _ := fixtureCtx(t, macArm)
	dmg := filepath.Join(ctx.DistDir(), "BrowserOS_v0.46.17_arm64.dmg")
	writeFile(t, dmg, "dmg-bytes-payload")

	module := SparkleSign{}
	if err := module.Validate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := module.Execute(ctx); err != nil {
		t.Fatal(err)
	}

	sig, ok := ctx.SparkleSignatures["BrowserOS_v0.46.17_arm64.dmg"]
	if !ok {
		t.Fatalf("signature not recorded: %v", ctx.SparkleSignatures)
	}
	if sig.Length != int64(len("dmg-bytes-payload")) {
		t.Errorf("length = %d", sig.Length)
	}
	sigBytes, err := base64.StdEncoding.DecodeString(sig.Signature)
	if err != nil {
		t.Fatal(err)
	}
	if !ed25519.Verify(key.Public().(ed25519.PublicKey), []byte("dmg-bytes-payload"), sigBytes) {
		t.Error("signature does not verify with the public key")
	}
}

func TestSparkleParsePrivateKeyFormats(t *testing.T) {
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i)
	}
	want := ed25519.NewKeyFromSeed(seed)

	// base64(32-byte seed)
	key, err := sparkle.ParsePrivateKey(base64.StdEncoding.EncodeToString(seed))
	if err != nil || !key.Equal(want) {
		t.Errorf("32-byte b64: %v", err)
	}
	// base64(64-byte seed+pub)
	full := append(append([]byte{}, seed...), want.Public().(ed25519.PublicKey)...)
	key, err = sparkle.ParsePrivateKey(base64.StdEncoding.EncodeToString(full))
	if err != nil || !key.Equal(want) {
		t.Errorf("64-byte b64: %v", err)
	}
	// Wrong length errors.
	if _, err := sparkle.ParsePrivateKey(base64.StdEncoding.EncodeToString([]byte("short"))); err == nil {
		t.Error("short key should error")
	}
}

func TestWindowsSignValidateAndServerPaths(t *testing.T) {
	ctx, _ := fixtureCtx(t, winX64)
	for _, name := range []string{"CODE_SIGN_TOOL_PATH", "ESIGNER_USERNAME", "ESIGNER_PASSWORD", "ESIGNER_TOTP_SECRET"} {
		t.Setenv(name, "")
		os.Unsetenv(name)
	}
	os.MkdirAll(ctx.OutDirAbs(), 0o755)

	if err := (WindowsSign{}).Validate(ctx); err == nil || !strings.Contains(err.Error(), "CODE_SIGN_TOOL_PATH") {
		t.Errorf("err = %v", err)
	}
	t.Setenv("CODE_SIGN_TOOL_PATH", t.TempDir())
	err := (WindowsSign{}).Validate(ctx)
	if err == nil || !strings.Contains(err.Error(), "ESIGNER_USERNAME") {
		t.Errorf("err = %v", err)
	}

	paths := ServerBinaryPaths(ctx.OutDirAbs())
	if len(paths) != 3 || !strings.HasSuffix(paths[0], "browseros_server.exe") {
		t.Errorf("server paths = %v", paths)
	}
	mac := MacOSSign{}
	if err := mac.Validate(ctx); err == nil || !strings.Contains(err.Error(), "requires macOS") {
		t.Errorf("macos sign on windows: %v", err)
	}
}

func TestLinuxSignIsNoOp(t *testing.T) {
	ctx, rec := fixtureCtx(t, platform.Platform{OS: "linux", Arch: "x64"})
	if err := (LinuxSign{}).Validate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := (LinuxSign{}).Execute(ctx); err != nil {
		t.Fatal(err)
	}
	if len(rec.Cmds) != 0 {
		t.Errorf("linux sign should run nothing: %v", rec.Argv())
	}
}
