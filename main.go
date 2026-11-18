package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"mywhoosh2garmin/garmin"
	"mywhoosh2garmin/mywhoosh"
)

//go:embed images/icon.png
var appIconBytes []byte

// ---------------------------------------------------------------------------
// App config (persisted to ~/.mywhoosh2garmin/config.json)
// ---------------------------------------------------------------------------

type appConfig struct {
	MyWhooshEmail string `json:"mywhoosh_email"`
	GarminEmail   string `json:"garmin_email"`
	GarminMFA     string `json:"garmin_mfa"`
	LastSyncAt    string `json:"last_sync_at"`
	LastSummary   string `json:"last_summary"`
	DeviceModel   string `json:"device_model"`  // "fenix6x" (default), "fenix6s", "none"
	DeviceSerial  string `json:"device_serial"` // optional unit id of your own watch
}

func appConfigDir() string {
	// On Android there is no usable home directory: use the app's private
	// storage (Fyne creates it), falling back to the cache/temp dir.
	if runtime.GOOS == "android" {
		if a := fyne.CurrentApp(); a != nil {
			if root := a.Storage().RootURI(); root != nil && root.Path() != "" {
				dir := filepath.Join(root.Path(), "mywhoosh2garmin")
				os.MkdirAll(dir, 0o700)
				return dir
			}
		}
		dir := filepath.Join(os.TempDir(), "mywhoosh2garmin")
		os.MkdirAll(dir, 0o700)
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".mywhoosh2garmin")
}

func loadAppConfig() appConfig {
	var cfg appConfig
	data, err := os.ReadFile(filepath.Join(appConfigDir(), "config.json"))
	if err == nil {
		json.Unmarshal(data, &cfg)
	}
	return cfg
}

func saveAppConfig(cfg appConfig) {
	dir := appConfigDir()
	os.MkdirAll(dir, 0o700)
	data, _ := json.MarshalIndent(cfg, "", "  ")
	os.WriteFile(filepath.Join(dir, "config.json"), data, 0o600)
}

// ---------------------------------------------------------------------------
// Synced-activity tracker (persisted to ~/.mywhoosh2garmin/synced.json)
// ---------------------------------------------------------------------------

type syncedTracker struct {
	mu       sync.Mutex
	path     string
	uploaded map[string]string // activityID → upload timestamp
}

func newSyncedTracker(dir string) *syncedTracker {
	st := &syncedTracker{
		path:     filepath.Join(dir, "synced.json"),
		uploaded: make(map[string]string),
	}
	data, err := os.ReadFile(st.path)
	if err == nil {
		json.Unmarshal(data, &st.uploaded)
	}
	return st
}

func (st *syncedTracker) IsSynced(activityID string) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	_, ok := st.uploaded[activityID]
	return ok
}

// Reset forgets every synced activity so they can be uploaded again.
func (st *syncedTracker) Reset() {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.uploaded = make(map[string]string)
	os.Remove(st.path)
}

func (st *syncedTracker) MarkSynced(activityID string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.uploaded[activityID] = time.Now().Format(time.RFC3339)
	data, _ := json.MarshalIndent(st.uploaded, "", "  ")
	os.WriteFile(st.path, data, 0o600)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

var (
	errNeedMW     = errors.New("MyWhoosh login required")
	errNeedGarmin = errors.New("Garmin login required")
	errNeedMFA    = errors.New("Garmin MFA code is required")
)

func isMFA(err error) bool {
	return err != nil && strings.Contains(err.Error(), "MFA code is required")
}

var logReplacer = strings.NewReplacer(
	"✓", "[OK]",
	"✅", "[OK]",
	"❌", "[ERROR]",
	"⚠", "[WARN]",
	"⬇", "[DOWNLOAD]",
	"⬆", "[UPLOAD]",
	"🔧", "[FIX]",
	"📧", "[EMAIL]",
	"⏳", "[WAIT]",
	"…", "...",
	"—", "-",
)

func plural(n int, one, many string) string {
	if n == 1 || n == 0 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// formatWhen renders a timestamp relative to today ("aujourd'hui 11:42").
func formatWhen(t time.Time) string {
	now := time.Now()
	if t.Year() == now.Year() && t.YearDay() == now.YearDay() {
		return "aujourd'hui " + t.Format("15:04")
	}
	return t.Format("02/01 15:04")
}

func lastSyncText(cfg appConfig) string {
	if cfg.LastSyncAt == "" {
		return "Pas encore de synchronisation"
	}
	t, err := time.Parse(time.RFC3339, cfg.LastSyncAt)
	if err != nil {
		return ""
	}
	text := "Dernière synchro : " + formatWhen(t)
	if cfg.LastSummary != "" {
		text += "\n" + cfg.LastSummary
	}
	return text
}

var deviceModels = []struct{ Key, Label string }{
	{"fenix6x", "Garmin Fenix 6X Pro / Solar"},
	{"fenix6s", "Garmin Fenix 6S Pro"},
}

func deviceLabel(key string) string {
	for _, d := range deviceModels {
		if d.Key == key {
			return d.Label
		}
	}
	return deviceModels[0].Label
}

func deviceKey(label string) string {
	for _, d := range deviceModels {
		if d.Label == label {
			return d.Key
		}
	}
	return deviceModels[0].Key
}

// applySpoof copies the device settings into the FIT fixer.
func applySpoof(cfg appConfig) {
	sp := deviceSpoof{Enabled: true, Product: productFenix6XPro, Serial: fakeSerialNumber}
	switch cfg.DeviceModel {
	case "fenix6s":
		sp.Product = productFenix6SPro
	case "none":
		sp.Enabled = false
	}
	if n, err := strconv.ParseUint(strings.TrimSpace(cfg.DeviceSerial), 10, 32); err == nil && n > 0 {
		sp.Serial = uint32(n)
	}
	spoofCfg = sp
}

type uploadOutcome int

const (
	outcomeSent uploadOutcome = iota
	outcomeDuplicate
	outcomeFailed
)

// ---------------------------------------------------------------------------
// GUI
// ---------------------------------------------------------------------------

func main() {
	a := app.New()
	a.SetIcon(fyne.NewStaticResource("icon.png", appIconBytes))
	w := a.NewWindow("MyWhoosh2Garmin")
	w.SetIcon(fyne.NewStaticResource("icon.png", appIconBytes))
	w.Resize(fyne.NewSize(420, 760))

	cfg := loadAppConfig()
	var cfgMu sync.Mutex
	updateCfg := func(mut func(c *appConfig)) {
		cfgMu.Lock()
		defer cfgMu.Unlock()
		mut(&cfg)
		saveAppConfig(cfg)
	}
	readCfg := func() appConfig {
		cfgMu.Lock()
		defer cfgMu.Unlock()
		return cfg
	}

	applySpoof(cfg)
	tracker := newSyncedTracker(appConfigDir())
	mwClient := mywhoosh.NewClient(appConfigDir())
	var garminClient *garmin.Client
	var busy atomic.Bool

	// --- Log -----------------------------------------------------------
	var logMu sync.Mutex
	var logLines []string
	logLabel := widget.NewLabel("")
	logLabel.Wrapping = fyne.TextWrapWord
	logLabel.TextStyle = fyne.TextStyle{Monospace: true}
	logScroll := container.NewVScroll(logLabel)
	logScroll.SetMinSize(fyne.NewSize(0, 160))

	appendLog := func(msg string) {
		msg = strings.TrimRight(logReplacer.Replace(msg), "\n")
		logMu.Lock()
		logLines = append(logLines, strings.Split(msg, "\n")...)
		if len(logLines) > 300 {
			logLines = logLines[len(logLines)-300:]
		}
		text := strings.Join(logLines, "\n")
		logMu.Unlock()
		fyne.Do(func() {
			logLabel.SetText(text)
			logScroll.ScrollToBottom()
		})
	}
	logFn = func(format string, args ...interface{}) {
		appendLog(fmt.Sprintf(format, args...))
	}
	mywhoosh.SetDebugFn(func(format string, args ...interface{}) {
		appendLog(fmt.Sprintf(format, args...))
	})

	// --- Settings entries (account details) -----------------------------
	mwEmailEntry := widget.NewEntry()
	mwEmailEntry.SetPlaceHolder("name@company.com")
	mwEmailEntry.SetText(cfg.MyWhooshEmail)
	mwPasswordEntry := widget.NewPasswordEntry()
	mwPasswordEntry.SetPlaceHolder("Mot de passe MyWhoosh")

	garminEmailEntry := widget.NewEntry()
	garminEmailEntry.SetPlaceHolder("name@company.com")
	garminEmailEntry.SetText(cfg.GarminEmail)
	garminPasswordEntry := widget.NewPasswordEntry()
	garminPasswordEntry.SetPlaceHolder("Mot de passe Garmin (première connexion)")
	garminMFAEntry := widget.NewEntry()
	garminMFAEntry.SetPlaceHolder("Code MFA Garmin à 6 chiffres")
	garminMFAEntry.Validator = func(value string) error {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil
		}
		if len(value) != 6 {
			return fmt.Errorf("saisissez 6 chiffres")
		}
		for _, r := range value {
			if r < '0' || r > '9' {
				return fmt.Errorf("chiffres uniquement")
			}
		}
		return nil
	}
	garminMFAEntry.SetText(cfg.GarminMFA)

	deviceLabels := make([]string, 0, len(deviceModels))
	for _, d := range deviceModels {
		deviceLabels = append(deviceLabels, d.Label)
	}
	deviceSelect := widget.NewSelect(deviceLabels, nil)
	deviceSelect.SetSelected(deviceLabel(cfg.DeviceModel))
	deviceSerialEntry := widget.NewEntry()
	deviceSerialEntry.SetPlaceHolder("Numéro de série / Unit ID (optionnel)")
	deviceSerialEntry.SetText(cfg.DeviceSerial)

	// --- Status pills ---------------------------------------------------
	setPill := func(label *widget.RichText, message string, importance widget.Importance) {
		colorName := theme.ColorNameForeground
		switch importance {
		case widget.SuccessImportance:
			colorName = theme.ColorNameSuccess
		case widget.WarningImportance:
			colorName = theme.ColorNameWarning
		case widget.DangerImportance:
			colorName = theme.ColorNameError
		case widget.HighImportance:
			colorName = theme.ColorNamePrimary
		}
		label.Wrapping = fyne.TextWrapOff
		label.Segments = []widget.RichTextSegment{
			&widget.TextSegment{
				Text:  message,
				Style: widget.RichTextStyle{ColorName: colorName, Alignment: fyne.TextAlignCenter},
			},
		}
		label.Refresh()
	}
	mwStatus := widget.NewRichText()
	garminStatus := widget.NewRichText()
	pendingPill := widget.NewRichText()
	setPill(mwStatus, "MyWhoosh : ...", widget.MediumImportance)
	setPill(garminStatus, "Garmin : ...", widget.MediumImportance)

	// --- Activities list (inside the journal panel) ---------------------
	activityBox := container.NewVBox()

	countUnsynced := func(acts []mywhoosh.Activity) int {
		n := 0
		for _, act := range acts {
			if !tracker.IsSynced(act.ID) {
				n++
			}
		}
		return n
	}

	renderActivities := func(acts []mywhoosh.Activity) {
		fyne.Do(func() {
			activityBox.RemoveAll()
			if len(acts) == 0 {
				activityBox.Add(widget.NewLabel("Aucune activité sur les 10 derniers jours."))
			}
			for _, act := range acts {
				details := act.FormattedDate() + "  -  " + act.DisplayName()
				if dist := act.FormattedDistance(); dist != "" {
					details += "  |  " + dist
				}
				if dur := act.FormattedDuration(); dur != "" {
					details += "  |  " + dur
				}
				if act.AvgPower > 0 {
					details += fmt.Sprintf("  |  %.0fW", act.AvgPower)
				}
				status := "En attente"
				if tracker.IsSynced(act.ID) {
					status = "Synchronisée"
				}
				info := widget.NewLabel(details)
				info.Wrapping = fyne.TextWrapWord
				activityBox.Add(container.NewBorder(nil, nil, nil, widget.NewLabel(status), info))
			}
			activityBox.Refresh()
		})
	}

	updatePendingPill := func(acts []mywhoosh.Activity) {
		n := countUnsynced(acts)
		fyne.Do(func() {
			if n > 0 {
				setPill(pendingPill, plural(n, "nouvelle", "nouvelles"), widget.HighImportance)
			} else {
				setPill(pendingPill, "À jour", widget.SuccessImportance)
			}
		})
	}

	// --- Main view widgets ----------------------------------------------
	var syncBtn *syncButton
	var runSync func()
	var showSettings func()

	syncBtn = newSyncButton(func() { runSync() })

	summary := widget.NewLabel(lastSyncText(cfg))
	summary.Alignment = fyne.TextAlignCenter
	summary.Wrapping = fyne.TextWrapWord

	actionBtn := widget.NewButton("", nil)
	actionBtn.Hide()

	setBtn := func(st syncState, p float64, title, sub string) {
		fyne.Do(func() { syncBtn.Set(st, p, title, sub) })
	}
	setSummary := func(msg string) {
		fyne.Do(func() { summary.SetText(msg) })
	}
	showAction := func(label string) {
		fyne.Do(func() {
			actionBtn.SetText(label)
			actionBtn.Show()
		})
	}
	hideAction := func() {
		fyne.Do(func() { actionBtn.Hide() })
	}

	// --- Account connection helpers --------------------------------------
	ensureMW := func() error {
		if err := mwClient.Resume(); err == nil {
			return nil
		}
		email, password := mwEmailEntry.Text, mwPasswordEntry.Text
		if email == "" || password == "" {
			return errNeedMW
		}
		appendLog("Connexion à MyWhoosh...")
		if err := mwClient.Login(email, password); err != nil {
			return fmt.Errorf("connexion MyWhoosh échouée : %w", err)
		}
		appendLog("✓ Connecté à MyWhoosh")
		return nil
	}

	fetchActivities := func() ([]mywhoosh.Activity, error) {
		if err := ensureMW(); err != nil {
			return nil, err
		}
		acts, err := mwClient.GetRecentActivities(10)
		if err != nil {
			// The cached session may have expired: retry with typed credentials.
			email, password := mwEmailEntry.Text, mwPasswordEntry.Text
			if email == "" || password == "" {
				return nil, fmt.Errorf("%w (%v)", errNeedMW, err)
			}
			appendLog("Session expirée, nouvelle connexion...")
			if lerr := mwClient.Login(email, password); lerr != nil {
				return nil, fmt.Errorf("connexion MyWhoosh échouée : %w", lerr)
			}
			acts, err = mwClient.GetRecentActivities(10)
			if err != nil {
				return nil, fmt.Errorf("récupération des activités échouée : %w", err)
			}
		}
		fyne.Do(func() { setPill(mwStatus, "MyWhoosh OK", widget.SuccessImportance) })
		return acts, nil
	}

	ensureGarmin := func() (*garmin.Client, error) {
		if garminClient != nil && garminClient.OAuth2 != nil && !garminClient.OAuth2.Expired() {
			return garminClient, nil
		}
		client := garmin.NewClient(appConfigDir())
		if err := client.Resume(); err == nil {
			garminClient = client
			fyne.Do(func() { setPill(garminStatus, "Garmin OK", widget.SuccessImportance) })
			return client, nil
		}
		email, password := garminEmailEntry.Text, garminPasswordEntry.Text
		if email == "" || password == "" {
			return nil, errNeedGarmin
		}
		appendLog("Connexion à Garmin...")
		err := client.LoginWithMFA(email, password, func(message string) (string, error) {
			code := strings.TrimSpace(garminMFAEntry.Text)
			if code == "" {
				return "", errNeedMFA
			}
			updateCfg(func(c *appConfig) { c.GarminMFA = code })
			return code, nil
		})
		if err != nil {
			if isMFA(err) {
				return nil, errNeedMFA
			}
			return nil, fmt.Errorf("connexion Garmin échouée : %w", err)
		}
		garminClient = client
		appendLog("✓ Connecté à Garmin")
		fyne.Do(func() { setPill(garminStatus, "Garmin OK", widget.SuccessImportance) })
		return client, nil
	}

	// handleFailure puts the UI in the "action required" state.
	handleFailure := func(err error) {
		appendLog("❌ " + err.Error())
		msg := err.Error()
		action := ""
		switch {
		case isMFA(err):
			msg = "Code MFA requis pour se connecter à Garmin"
			action = "Saisir le code MFA"
			fyne.Do(func() { setPill(garminStatus, "Garmin : MFA", widget.WarningImportance) })
		case errors.Is(err, errNeedMW):
			msg = "Connexion MyWhoosh requise"
			action = "Ouvrir les réglages"
			fyne.Do(func() { setPill(mwStatus, "MyWhoosh : à connecter", widget.WarningImportance) })
		case errors.Is(err, errNeedGarmin):
			msg = "Connexion Garmin requise"
			action = "Ouvrir les réglages"
			fyne.Do(func() { setPill(garminStatus, "Garmin : à connecter", widget.WarningImportance) })
		}
		setBtn(stateError, 0, "Réessayer", "")
		setSummary(msg)
		if action != "" {
			showAction(action)
		}
	}

	// --- Upload of a single activity -------------------------------------
	uploadOne := func(client *garmin.Client, act mywhoosh.Activity) uploadOutcome {
		appendLog(fmt.Sprintf("⬇ Téléchargement : %s (%s)...", act.DisplayName(), act.FormattedDate()))
		fileID := act.ActivityFileID
		if fileID == "" {
			fileID = act.ID
		}
		fitData, err := mwClient.DownloadFitFile(fileID)
		if err != nil {
			appendLog("  ❌ Téléchargement échoué : " + err.Error())
			return outcomeFailed
		}
		appendLog(fmt.Sprintf("  ✓ %d octets", len(fitData)))

		tmpDir := filepath.Join(appConfigDir(), "tmp")
		os.MkdirAll(tmpDir, 0o700)
		ts := time.Now().Format("20060102_150405")
		inputPath := filepath.Join(tmpDir, fmt.Sprintf("mw_%s_%s.fit", act.ID, ts))
		outputPath := filepath.Join(tmpDir, fmt.Sprintf("mw_%s_%s_fixed.fit", act.ID, ts))
		if err := os.WriteFile(inputPath, fitData, 0o600); err != nil {
			appendLog("  ❌ Écriture du fichier échouée : " + err.Error())
			return outcomeFailed
		}
		defer os.Remove(inputPath)
		defer os.Remove(outputPath)

		appendLog("  🔧 Correction du fichier FIT...")
		if err := fixFitFile(inputPath, outputPath); err != nil {
			appendLog("  ❌ Correction échouée : " + err.Error())
			return outcomeFailed
		}

		appendLog("  ⬆ Envoi vers Garmin Connect...")
		if err := client.UploadFIT(outputPath); err != nil {
			if strings.Contains(err.Error(), "duplicate") {
				appendLog("  ⚠ Déjà sur Garmin (doublon)")
				tracker.MarkSynced(act.ID)
				return outcomeDuplicate
			}
			appendLog("  ❌ Envoi échoué : " + err.Error())
			return outcomeFailed
		}
		tracker.MarkSynced(act.ID)
		appendLog("  ✓ Envoyée")
		return outcomeSent
	}

	// --- One-tap sync ------------------------------------------------------
	runSync = func() {
		if !busy.CompareAndSwap(false, true) {
			return
		}
		updateCfg(func(c *appConfig) {
			c.MyWhooshEmail = mwEmailEntry.Text
			c.GarminEmail = garminEmailEntry.Text
		})
		go func() {
			defer busy.Store(false)
			hideAction()
			setBtn(stateRunning, 0.05, "...", "Connexion")
			setSummary("Connexion à MyWhoosh...")

			acts, err := fetchActivities()
			if err != nil {
				handleFailure(err)
				return
			}
			renderActivities(acts)
			updatePendingPill(acts)

			var todo []mywhoosh.Activity
			for _, act := range acts {
				if !tracker.IsSynced(act.ID) {
					todo = append(todo, act)
				}
			}

			sent, dup, failed := 0, 0, 0
			if len(todo) > 0 {
				client, err := ensureGarmin()
				if err != nil {
					handleFailure(err)
					return
				}
				for i, act := range todo {
					setBtn(stateRunning, (float64(i)+0.5)/float64(len(todo)),
						fmt.Sprintf("%d/%d", i+1, len(todo)), "Envoi")
					setSummary(fmt.Sprintf("Activité %d sur %d : %s", i+1, len(todo), act.DisplayName()))
					switch uploadOne(client, act) {
					case outcomeSent:
						sent++
					case outcomeDuplicate:
						dup++
					default:
						failed++
					}
				}
			}

			var parts []string
			if sent > 0 {
				parts = append(parts, plural(sent, "envoyée", "envoyées"))
			}
			if dup > 0 {
				parts = append(parts, plural(dup, "déjà sur Garmin", "déjà sur Garmin"))
			}
			if failed > 0 {
				parts = append(parts, plural(failed, "échec", "échecs"))
			}
			result := "Rien de nouveau"
			if len(parts) > 0 {
				result = strings.Join(parts, ", ")
			}
			updateCfg(func(c *appConfig) {
				c.LastSyncAt = time.Now().Format(time.RFC3339)
				c.LastSummary = result
			})
			renderActivities(acts)
			updatePendingPill(acts)
			if failed == 0 {
				setBtn(stateDone, 1, "À jour", "")
			} else {
				setBtn(stateError, 0, "Réessayer", "")
			}
			setSummary(lastSyncText(readCfg()))
		}()
	}

	// --- Silent check at startup / after saving settings ---------------------
	refresh := func() {
		if !busy.CompareAndSwap(false, true) {
			return
		}
		go func() {
			defer busy.Store(false)
			hideAction()
			if gc := garmin.NewClient(appConfigDir()); gc.Resume() == nil {
				garminClient = gc
				fyne.Do(func() { setPill(garminStatus, "Garmin OK", widget.SuccessImportance) })
			} else {
				fyne.Do(func() { setPill(garminStatus, "Garmin : à connecter", widget.WarningImportance) })
			}
			acts, err := fetchActivities()
			if err != nil {
				handleFailure(err)
				return
			}
			renderActivities(acts)
			updatePendingPill(acts)
			setBtn(stateIdle, 0, "Synchroniser", "")
			setSummary(lastSyncText(readCfg()))
		}()
	}

	// --- Journal / activities panel ---------------------------------------
	details := container.NewVBox(
		widget.NewRichTextFromMarkdown("### Activités (10 derniers jours)"),
		activityBox,
		widget.NewSeparator(),
		widget.NewRichTextFromMarkdown("### Journal"),
		logScroll,
	)
	details.Hide()
	toggle := widget.NewButton("Afficher le journal", nil)
	toggle.Importance = widget.LowImportance

	// --- Main view ------------------------------------------------------------
	settingsBtn := widget.NewButtonWithIcon("", theme.SettingsIcon(), func() { showSettings() })
	settingsBtn.Importance = widget.LowImportance
	header := container.NewBorder(nil, nil, nil, settingsBtn,
		widget.NewRichTextFromMarkdown("## MyWhoosh → Garmin"))
	statusRow := container.NewCenter(container.NewHBox(mwStatus, garminStatus))

	center := container.NewBorder(
		container.NewCenter(pendingPill), nil, nil, nil,
		container.New(squareLayout{}, syncBtn),
	)
	mainBlock := container.NewBorder(
		container.NewVBox(header, statusRow), container.NewVBox(summary, actionBtn), nil, nil,
		center,
	)

	adaptive := &adaptiveLayout{}
	content := container.New(adaptive, mainBlock, toggle, details)
	mainView := container.NewVScroll(content)

	relayout := func() {
		content.Refresh()
		mainView.Refresh()
	}
	toggle.OnTapped = func() {
		if details.Visible() {
			details.Hide()
			toggle.SetText("Afficher le journal")
		} else {
			details.Show()
			toggle.SetText("Masquer le journal")
		}
		relayout()
	}
	adaptive.onMode = func(wide bool) {
		fyne.Do(func() {
			if wide {
				details.Show()
				toggle.Hide()
			} else {
				details.Hide()
				toggle.SetText("Afficher le journal")
				toggle.Show()
			}
			relayout()
		})
	}

	// --- Settings view ------------------------------------------------------------
	settingsMsg := widget.NewLabel("")
	settingsMsg.Wrapping = fyne.TextWrapWord

	showMain := func() { w.SetContent(mainView) }

	var saveBtn *widget.Button
	saveBtn = widget.NewButton("Enregistrer et tester", func() {
		if err := garminMFAEntry.Validate(); err != nil {
			settingsMsg.SetText("Code MFA invalide : " + err.Error())
			return
		}
		if !busy.CompareAndSwap(false, true) {
			settingsMsg.SetText("Une synchronisation est en cours, réessayez dans un instant.")
			return
		}
		updateCfg(func(c *appConfig) {
			c.MyWhooshEmail = mwEmailEntry.Text
			c.GarminEmail = garminEmailEntry.Text
			c.GarminMFA = strings.TrimSpace(garminMFAEntry.Text)
			c.DeviceModel = deviceKey(deviceSelect.Selected)
			c.DeviceSerial = strings.TrimSpace(deviceSerialEntry.Text)
		})
		applySpoof(readCfg())
		settingsMsg.SetText("Test des connexions...")
		saveBtn.Disable()
		go func() {
			var problems []string
			if err := ensureMW(); err != nil {
				problems = append(problems, "MyWhoosh : "+err.Error())
				fyne.Do(func() { setPill(mwStatus, "MyWhoosh : à connecter", widget.WarningImportance) })
			} else {
				fyne.Do(func() { setPill(mwStatus, "MyWhoosh OK", widget.SuccessImportance) })
			}
			if _, err := ensureGarmin(); err != nil {
				msg := err.Error()
				if isMFA(err) {
					msg = "code MFA requis (saisissez le code reçu par e-mail puis réessayez)"
					fyne.Do(func() { setPill(garminStatus, "Garmin : MFA", widget.WarningImportance) })
				} else {
					fyne.Do(func() { setPill(garminStatus, "Garmin : à connecter", widget.WarningImportance) })
				}
				problems = append(problems, "Garmin : "+msg)
			}
			busy.Store(false)
			fyne.Do(func() {
				saveBtn.Enable()
				if len(problems) > 0 {
					settingsMsg.SetText(strings.Join(problems, "\n"))
					return
				}
				settingsMsg.SetText("")
				showMain()
			})
			if len(problems) == 0 {
				refresh()
			}
		}()
	})
	saveBtn.Importance = widget.HighImportance

	backBtn := widget.NewButtonWithIcon("Retour", theme.NavigateBackIcon(), func() { showMain() })
	backBtn.Importance = widget.LowImportance

	resetBtn := widget.NewButton("Oublier les activités déjà synchronisées", func() {
		tracker.Reset()
		settingsMsg.SetText("Liste effacée. Supprimez d'abord l'activité sur Garmin Connect, sinon Garmin la refusera comme doublon.")
	})

	settingsForm := container.NewVBox(
		container.NewHBox(backBtn),
		widget.NewRichTextFromMarkdown("## Réglages"),
		widget.NewLabel("Compte MyWhoosh"),
		mwEmailEntry,
		mwPasswordEntry,
		widget.NewSeparator(),
		widget.NewLabel("Compte Garmin Connect"),
		garminEmailEntry,
		garminPasswordEntry,
		garminMFAEntry,
		widget.NewLabel("Le mot de passe Garmin et le code MFA ne servent qu'à la première connexion."),
		widget.NewSeparator(),
		widget.NewLabel("Appareil présenté à Garmin"),
		deviceSelect,
		deviceSerialEntry,
		widget.NewLabel("Avec votre propre numéro de série (Garmin Connect > Appareils > Infos), l'activité est rattachée à votre vraie montre. Vos capteurs (cardiaque, puissance) ne sont pas modifiés."),
		resetBtn,
		saveBtn,
		settingsMsg,
	)
	settingsView := container.NewVScroll(container.NewPadded(container.New(maxWidthLayout{max: 480}, settingsForm)))

	showSettings = func() { w.SetContent(settingsView) }
	actionBtn.OnTapped = func() { showSettings() }

	if cfg.MyWhooshEmail == "" {
		showSettings()
	} else {
		showMain()
		refresh()
	}
	w.ShowAndRun()
}
