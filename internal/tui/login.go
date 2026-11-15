package tui

import (
	"context"
	"fmt"
	"image"
	"image/png"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"yhchat-tui/internal/api"
	"yhchat-tui/internal/captchaimg"
	"yhchat-tui/internal/config"
)

// loginMethod is the selected login tab.
type loginMethod int

const (
	methodEmail loginMethod = iota
	methodSMS
)

// smsStage tracks the multi-step SMS login flow.
type smsStage int

const (
	smsStagePhone   smsStage = iota // enter mobile number, then fetch captcha
	smsStageCaptcha                 // read image captcha, then send SMS
	smsStageCode                    // enter SMS code, then login
)

// focusID identifies the focused widget on the login screen.
type focusID int

const (
	focusTabs focusID = iota
	focusEmail
	focusPassword
	focusPhone
	focusImageCode
	focusSMSCode
	focusSubmit
)

const (
	maxCaptchaRows = 14
	requestTimeout = api.DefaultTimeout + 15*time.Second
)

// loginModel is the login screen.
type loginModel struct {
	client *api.Client

	width, height int

	method loginMethod
	stage  smsStage
	focus  focusID

	email     textinput.Model
	password  textinput.Model
	phone     textinput.Model
	imageCode textinput.Model
	smsCode   textinput.Model
	spinner   spinner.Model

	captcha *api.ImageCaptcha
	capImg  image.Image

	busy     bool
	busyText string
	errText  string
	okText   string
	notice   string // persistent hint from the App (e.g. session expired)

	useColor bool
}

// newLoginModel builds the login screen, restoring the last used method and
// account names.
func newLoginModel(client *api.Client, prefs config.Config) loginModel {
	m := loginModel{
		client:    client,
		method:    methodEmail,
		stage:     smsStagePhone,
		focus:     focusEmail,
		email:     newTextInput("you@example.com", false, 128),
		password:  newTextInput("password", true, 128),
		phone:     newTextInput("phone number", false, 11),
		imageCode: newTextInput("captcha text", false, 16),
		smsCode:   newTextInput("SMS code", false, 16),
		spinner:   newSpinner(),
		useColor:  colorEnabled(),
	}
	if prefs.LoginMethod == "sms" {
		m.method = methodSMS
		m.focus = focusPhone
	}
	if prefs.LastEmail != "" {
		m.email.SetValue(prefs.LastEmail)
	}

	return m
}

// newTextInput creates a styled single-line input.
func newTextInput(placeholder string, secret bool, limit int) textinput.Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = placeholder
	ti.CharLimit = limit
	if secret {
		ti.EchoMode = textinput.EchoPassword
	}
	ti.PlaceholderStyle = lipgloss.NewStyle().Foreground(colorMuted)
	ti.TextStyle = lipgloss.NewStyle().Foreground(colorText)
	ti.PromptStyle = lipgloss.NewStyle()
	ti.Cursor.Style = lipgloss.NewStyle().Foreground(colorAccent)
	return ti
}

// colorEnabled reports whether 24-bit ANSI colors are safe to emit.
func colorEnabled() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if strings.EqualFold(os.Getenv("TERM"), "dumb") {
		return false
	}
	return true
}

// start focuses the initial widget and starts the spinner.
func (m loginModel) start() (loginModel, tea.Cmd) {
	cmd := m.applyFocus()
	return m, tea.Batch(cmd, m.spinner.Tick)
}

// resize records the terminal size.
func (m loginModel) resize(width, height int) loginModel {
	m.width, m.height = width, height
	return m
}

// input returns a pointer to the text input identified by id (nil for
// non-input focus targets).
func (m *loginModel) input(id focusID) *textinput.Model {
	switch id {
	case focusEmail:
		return &m.email
	case focusPassword:
		return &m.password
	case focusPhone:
		return &m.phone
	case focusImageCode:
		return &m.imageCode
	case focusSMSCode:
		return &m.smsCode
	default:
		return nil
	}
}

// applyFocus blurs every input and focuses the current one.
func (m *loginModel) applyFocus() tea.Cmd {
	for _, id := range []focusID{focusEmail, focusPassword, focusPhone, focusImageCode, focusSMSCode} {
		m.input(id).Blur()
	}
	if ti := m.input(m.focus); ti != nil {
		return ti.Focus()
	}
	return nil
}

// focusRing returns the focusable widgets for the current form state.
func (m loginModel) focusRing() []focusID {
	ring := []focusID{focusTabs}
	if m.method == methodEmail {
		return append(ring, focusEmail, focusPassword, focusSubmit)
	}
	ring = append(ring, focusPhone)
	if m.stage >= smsStageCaptcha {
		ring = append(ring, focusImageCode)
	}
	if m.stage >= smsStageCode {
		ring = append(ring, focusSMSCode)
	}
	return append(ring, focusSubmit)
}

// nextFocus moves the focus through the ring.
func (m loginModel) nextFocus(delta int) focusID {
	ring := m.focusRing()
	idx := 0
	for i, id := range ring {
		if id == m.focus {
			idx = i
			break
		}
	}
	n := len(ring)
	idx = ((idx+delta)%n + n) % n
	return ring[idx]
}

// firstField is the first editable widget of the current method.
func (m loginModel) firstField() focusID {
	if m.method == methodEmail {
		return focusEmail
	}
	return focusPhone
}

// Init implements tea.Model.
func (m loginModel) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model for the login screen.
func (m loginModel) Update(msg tea.Msg) (loginModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.resize(msg.Width, msg.Height), nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case captchaLoadedMsg:
		m.busy = false
		if msg.err != nil {
			m.errText = api.FriendlyMessage(msg.err)
			m.okText = ""
			return m, nil
		}
		m.captcha = msg.captcha
		m.capImg = msg.img
		m.stage = smsStageCaptcha
		m.imageCode.SetValue("")
		m.errText = ""
		m.okText = "请输入图片中的字符，然后发送短信验证码"
		m.focus = focusImageCode
		return m, m.applyFocus()

	case smsSentMsg:
		m.busy = false
		if msg.err != nil {
			m.errText = api.FriendlyMessage(msg.err)
			m.okText = ""
			m.focus = focusImageCode
			return m, m.applyFocus()
		}
		m.stage = smsStageCode
		m.smsCode.SetValue("")
		m.errText = ""
		m.okText = "短信验证码已发送"
		m.focus = focusSMSCode
		return m, m.applyFocus()

	case loginFailedMsg:
		m.busy = false
		m.errText = api.FriendlyMessage(msg.err)
		m.okText = ""
		return m, nil

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case tea.KeyMsg:
		return m.handleKey(msg)

	default:
		// Cursor blink / paste events are handled by the focused input.
		if m.busy {
			return m, nil
		}
		ti := m.input(m.focus)
		if ti == nil {
			return m, nil
		}
		updated, cmd := ti.Update(msg)
		*ti = updated
		return m, cmd
	}
}

// handleKey processes key presses, intercepting navigation keys before they
// reach the focused text input.
func (m loginModel) handleKey(msg tea.KeyMsg) (loginModel, tea.Cmd) {
	key := msg.String()

	if m.busy {
		return m, nil
	}

	switch key {
	case "tab", "down", "ctrl+n":
		m.focus = m.nextFocus(1)
		return m, m.applyFocus()
	case "shift+tab", "up", "ctrl+p":
		m.focus = m.nextFocus(-1)
		return m, m.applyFocus()
	case "ctrl+t":
		m.focus = focusTabs
		return m, m.applyFocus()
	case "enter":
		return m.activate()
	case "esc":
		return m.handleBack()
	case "ctrl+r":
		if m.method == methodSMS {
			return m.refreshCaptcha()
		}
		return m, nil
	case "ctrl+s":
		return m.saveCaptcha()
	}

	if m.focus == focusTabs {
		switch key {
		case "left", "h":
			return m.switchMethod(methodEmail)
		case "right", "l":
			return m.switchMethod(methodSMS)
		case "1":
			return m.switchMethod(methodEmail)
		case "2":
			return m.switchMethod(methodSMS)
		}
		return m, nil
	}

	ti := m.input(m.focus)
	if ti == nil {
		return m, nil
	}
	updated, cmd := ti.Update(msg)
	*ti = updated
	return m, cmd
}

// handleBack implements Esc (one step back in the SMS flow).
func (m loginModel) handleBack() (loginModel, tea.Cmd) {
	if m.notice != "" {
		m.notice = ""
		return m, nil
	}
	if m.errText != "" {
		m.errText = ""
		return m, nil
	}
	if m.method == methodSMS && m.stage != smsStagePhone {
		m.stage--
		if m.stage == smsStagePhone {
			m.captcha = nil
			m.capImg = nil
			m.imageCode.SetValue("")
			m.smsCode.SetValue("")
		}
		m.okText = ""
		m.focus = focusPhone
		return m, m.applyFocus()
	}
	return m, nil
}

// switchMethod changes the login tab, fetching a captcha when needed.
func (m loginModel) switchMethod(target loginMethod) (loginModel, tea.Cmd) {
	if m.busy || m.method == target {
		return m, nil
	}
	m.method = target
	m.errText = ""
	m.okText = ""
	m.notice = ""
	m.focus = m.firstField()
	cmd := m.applyFocus()
	if target == methodSMS && m.captcha == nil && m.stage == smsStagePhone {
		var fetchCmd tea.Cmd
		m, fetchCmd = m.fetchCaptcha()
		cmd = tea.Batch(cmd, fetchCmd)
	}
	return m, cmd
}

// activate handles Enter / button presses for the current form state.
func (m loginModel) activate() (loginModel, tea.Cmd) {
	if m.busy {
		return m, nil
	}
	if m.focus == focusTabs {
		m.focus = m.firstField()
		return m, m.applyFocus()
	}
	if m.method == methodEmail {
		return m.submitEmail()
	}
	switch m.stage {
	case smsStagePhone:
		return m.fetchCaptcha()
	case smsStageCaptcha:
		return m.sendSMSCode()
	default:
		return m.submitSMSLogin()
	}
}

// fetchCaptcha requests a new image captcha from the server.
func (m loginModel) fetchCaptcha() (loginModel, tea.Cmd) {
	if m.busy {
		return m, nil
	}
	m.busy = true
	m.busyText = "正在获取图形验证码…"
	m.errText = ""
	m.okText = ""
	return m, tea.Batch(m.spinner.Tick, m.fetchCaptchaCmd())
}

// refreshCaptcha discards the old captcha and loads a new one.
func (m loginModel) refreshCaptcha() (loginModel, tea.Cmd) {
	m.captcha = nil
	m.capImg = nil
	m.smsCode.SetValue("")
	m.stage = smsStagePhone
	return m.fetchCaptcha()
}

// fetchCaptchaCmd performs the captcha request (and decodes the image) in the
// background.
func (m loginModel) fetchCaptchaCmd() tea.Cmd {
	client := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		captcha, err := client.FetchImageCaptcha(ctx)
		if err != nil {
			return captchaLoadedMsg{err: err}
		}
		img, err := captchaimg.DecodeDataURL(captcha.DataURL)
		if err != nil {
			return captchaLoadedMsg{err: fmt.Errorf("验证码图片解析失败: %w", err)}
		}
		return captchaLoadedMsg{captcha: captcha, img: img}
	}
}

// sendSMSCode validates the form and asks the server to send an SMS code.
func (m loginModel) sendSMSCode() (loginModel, tea.Cmd) {
	mobile := strings.TrimSpace(m.phone.Value())
	if err := validateMobile(mobile); err != nil {
		m.errText = err.Error()
		m.focus = focusPhone
		return m, m.applyFocus()
	}
	imageCode := strings.TrimSpace(m.imageCode.Value())
	if imageCode == "" {
		m.errText = "请输入图片中的验证码"
		m.focus = focusImageCode
		return m, m.applyFocus()
	}
	if m.captcha == nil {
		return m.fetchCaptcha()
	}
	m.busy = true
	m.busyText = "正在发送短信验证码…"
	m.errText = ""
	m.okText = ""

	client, captchaID := m.client, m.captcha.ID
	cmd := func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		return smsSentMsg{err: client.SendSMSCode(ctx, mobile, imageCode, captchaID)}
	}
	return m, tea.Batch(m.spinner.Tick, cmd)
}

// submitEmail validates the email form and logs in.
func (m loginModel) submitEmail() (loginModel, tea.Cmd) {
	email := strings.TrimSpace(m.email.Value())
	password := m.password.Value()
	if err := validateEmail(email); err != nil {
		m.errText = err.Error()
		m.focus = focusEmail
		return m, m.applyFocus()
	}
	if password == "" {
		m.errText = "请输入密码"
		m.focus = focusPassword
		return m, m.applyFocus()
	}

	m.busy = true
	m.busyText = "正在登录…"
	m.errText = ""
	m.okText = ""

	client := m.client
	cmd := func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		token, err := client.LoginEmail(ctx, email, password)
		if err != nil {
			return loginFailedMsg{err: err}
		}
		user, userErr := client.UserInfo(ctx, token)
		return loginSucceededMsg{token: token, user: user, userErr: userErr, method: "email", account: email}
	}
	return m, tea.Batch(m.spinner.Tick, cmd)
}

// submitSMSLogin validates the SMS code and logs in.
func (m loginModel) submitSMSLogin() (loginModel, tea.Cmd) {
	mobile := strings.TrimSpace(m.phone.Value())
	if err := validateMobile(mobile); err != nil {
		m.errText = err.Error()
		m.focus = focusPhone
		return m, m.applyFocus()
	}
	code := strings.TrimSpace(m.smsCode.Value())
	if len([]rune(code)) < 4 {
		m.errText = "请输入短信验证码"
		m.focus = focusSMSCode
		return m, m.applyFocus()
	}

	m.busy = true
	m.busyText = "正在登录…"
	m.errText = ""
	m.okText = ""

	client := m.client
	cmd := func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		token, err := client.LoginSMS(ctx, mobile, code)
		if err != nil {
			return loginFailedMsg{err: err}
		}
		user, userErr := client.UserInfo(ctx, token)
		return loginSucceededMsg{token: token, user: user, userErr: userErr, method: "sms", account: mobile}
	}
	return m, tea.Batch(m.spinner.Tick, cmd)
}

// saveCaptcha writes the current captcha image to a temp PNG so the user can
// open it in an image viewer if the terminal rendering is hard to read.
func (m loginModel) saveCaptcha() (loginModel, tea.Cmd) {
	if m.capImg == nil {
		m.errText = "当前没有可保存的验证码图片"
		return m, nil
	}
	f, err := os.CreateTemp("", "yhchat-captcha-*.png")
	if err != nil {
		m.errText = "保存验证码图片失败: " + err.Error()
		return m, nil
	}
	path := f.Name()
	encodeErr := png.Encode(f, m.capImg)
	closeErr := f.Close()
	if encodeErr != nil {
		m.errText = "写入验证码图片失败: " + encodeErr.Error()
		return m, nil
	}
	if closeErr != nil {
		m.errText = "保存验证码图片失败: " + closeErr.Error()
		return m, nil
	}
	m.okText = "验证码图片已保存: " + path
	return m, nil
}

// submitLabel is the text of the primary button for the current state.
func (m loginModel) submitLabel() string {
	if m.method == methodEmail {
		return "登 录"
	}
	switch m.stage {
	case smsStagePhone:
		return "获取图形验证码"
	case smsStageCaptcha:
		return "发送短信验证码"
	default:
		return "登 录"
	}
}

// View implements tea.Model for the login screen. The screen fills the whole
// terminal: header, body, and a bottom hint bar (see screenChrome).
func (m loginModel) View() string {
	width, height := screenSize(m.width, m.height)
	inner := width - 2*chromePadX
	body := m.bodyLines(inner, height-4)

	headerRight := subtitleStyle.Render("邮箱密码登录")
	if m.method == methodSMS {
		headerRight = subtitleStyle.Render("短信验证码登录")
	}
	return screenChrome(width, height,
		titleAccentStyle.Render("▌ 云湖 TUI"),
		headerRight,
		body,
		m.viewHints(),
	)
}

// bodyLines builds the form for the current state and fits it into avail rows:
// decorative spacing is dropped first, then the captcha image shrinks.
func (m loginModel) bodyLines(inner, avail int) []string {
	if m.method == methodEmail {
		if roomy := m.emailForm(inner, true); layoutHeight(roomy) <= avail {
			return roomy
		}
		return m.emailForm(inner, false)
	}

	switch m.stage {
	case smsStageCaptcha:
		// The captcha is the readability-critical part: on short terminals use
		// the compact form so the image keeps more rows.
		if avail >= 28 {
			return m.smsCaptchaForm(inner, true, clamp(avail-16, 3, maxCaptchaRows))
		}
		return m.smsCaptchaForm(inner, false, clamp(avail-13, 3, maxCaptchaRows))
	case smsStageCode:
		if roomy := m.smsCodeForm(inner, true); layoutHeight(roomy) <= avail {
			return roomy
		}
		return m.smsCodeForm(inner, false)
	default:
		if roomy := m.smsPhoneForm(inner, true); layoutHeight(roomy) <= avail {
			return roomy
		}
		return m.smsPhoneForm(inner, false)
	}
}

// emailForm renders the email/password form.
func (m loginModel) emailForm(inner int, roomy bool) []string {
	lines := []string{m.viewTabs(inner)}
	if roomy {
		lines = append(lines, "")
	}
	lines = append(lines,
		m.viewLabel("邮箱", m.focus == focusEmail, inner),
		m.viewInput(focusEmail, inner),
	)
	if roomy {
		lines = append(lines, "")
	}
	lines = append(lines,
		m.viewLabel("密码", m.focus == focusPassword, inner),
		m.viewInput(focusPassword, inner),
	)
	if roomy {
		lines = append(lines, "")
	}
	lines = append(lines, m.viewButton(m.submitLabel(), inner), m.viewStatusLine(inner))
	return lines
}

// smsPhoneForm renders the first SMS step (mobile number only).
func (m loginModel) smsPhoneForm(inner int, roomy bool) []string {
	lines := []string{m.viewTabs(inner)}
	if roomy {
		lines = append(lines, "")
	}
	lines = append(lines,
		m.viewLabel("手机号", m.focus == focusPhone, inner),
		m.viewInput(focusPhone, inner),
	)
	if roomy {
		lines = append(lines, "")
	}
	lines = append(lines, m.viewButton(m.submitLabel(), inner), m.viewStatusLine(inner))
	return lines
}

// smsCaptchaForm renders the SMS step where the image captcha is shown.
func (m loginModel) smsCaptchaForm(inner int, roomy bool, captchaRows int) []string {
	lines := []string{m.viewTabs(inner)}
	if roomy {
		lines = append(lines, "")
	}
	lines = append(lines,
		m.viewLabel("手机号", m.focus == focusPhone, inner),
		m.viewInput(focusPhone, inner),
	)
	if roomy {
		lines = append(lines, "")
	}
	lines = append(lines,
		m.viewLabel("图形验证码", m.focus == focusImageCode, inner),
	)
	lines = append(lines, m.viewCaptcha(inner, captchaRows)...)
	lines = append(lines, m.viewInput(focusImageCode, inner))
	if roomy {
		lines = append(lines, "")
	}
	lines = append(lines, m.viewButton(m.submitLabel(), inner), m.viewStatusLine(inner))
	return lines
}

// smsCodeForm renders the final SMS step (code entry). The captcha image is no
// longer shown: it has already been used to send the SMS.
func (m loginModel) smsCodeForm(inner int, roomy bool) []string {
	lines := []string{m.viewTabs(inner)}
	if roomy {
		lines = append(lines, "")
	}
	lines = append(lines,
		m.viewLabel("手机号", m.focus == focusPhone, inner),
		m.viewInput(focusPhone, inner),
	)
	if roomy {
		lines = append(lines, "")
	}
	lines = append(lines,
		m.viewLabel("短信验证码", m.focus == focusSMSCode, inner),
		m.viewInput(focusSMSCode, inner),
	)
	if roomy {
		lines = append(lines, "")
	}
	lines = append(lines, m.viewButton(m.submitLabel(), inner), m.viewStatusLine(inner))
	return lines
}

// viewTabs renders the login method selector, left aligned like the form.
func (m loginModel) viewTabs(inner int) string {
	emailStyle, smsStyle := tabInactiveStyle, tabInactiveStyle
	if m.method == methodEmail {
		emailStyle = tabActiveStyle
	} else {
		smsStyle = tabActiveStyle
	}
	row := lipgloss.JoinHorizontal(lipgloss.Top,
		emailStyle.Render("邮箱密码"),
		" ",
		smsStyle.Render("短信验证码"),
	)
	if m.focus == focusTabs {
		row = lipgloss.JoinHorizontal(lipgloss.Top,
			titleAccentStyle.Render("‹"),
			" ",
			row,
			" ",
			titleAccentStyle.Render("›"),
		)
	}
	return "  " + row
}

// viewLabel renders a field label.
func (m loginModel) viewLabel(label string, focused bool, inner int) string {
	style := labelStyle
	prefix := "  "
	if focused {
		style = labelFocusedStyle
		prefix = "» "
	}
	return truncate(style.Render(prefix+label), inner)
}

// viewInput renders one text input line with a focus underline.
func (m loginModel) viewInput(id focusID, inner int) string {
	ti := m.input(id)
	if ti == nil {
		return ""
	}
	focused := m.focus == id && !m.busy
	input := *ti
	input.Width = maxInt(4, inner-5)
	style := inputStyle
	if focused {
		style = inputFocusedStyle
	}
	return style.Width(inner).Render(input.View())
}

// viewButton renders the primary action button.
func (m loginModel) viewButton(label string, inner int) string {
	style := buttonStyle
	switch {
	case m.busy:
		style = buttonDisabledStyle
	case m.focus == focusSubmit:
		style = buttonFocusedStyle
	}
	return lipgloss.PlaceHorizontal(inner, lipgloss.Center, style.Render(label))
}

// viewCaptcha renders the captcha image inside a bordered box, centred
// horizontally.
//
// Note on widths: lipgloss.Style.Width sets the content width including
// padding, and a border adds one cell on each side, so the content budget for
// the image is inner-4 (inner = usable body width). Lines wider than that would
// make lipgloss word-wrap the image, so the budget must be exact.
func (m loginModel) viewCaptcha(inner int, maxRows int) []string {
	if m.capImg == nil {
		rule := hintStyle.Render("（图形验证码加载中…）")
		return []string{lipgloss.PlaceHorizontal(inner, lipgloss.Center, rule)}
	}
	colsBudget := maxInt(24, inner-4)
	rendered := captchaimg.Render(m.capImg, captchaimg.Options{
		MaxCols: colsBudget,
		MaxRows: maxRows,
		Color:   m.useColor,
	})
	if rendered == "" {
		rule := hintStyle.Render("（验证码图片无法渲染，Ctrl+S 保存后查看）")
		return []string{lipgloss.PlaceHorizontal(inner, lipgloss.Center, rule)}
	}
	cols := 0
	for _, line := range strings.Split(rendered, "\n") {
		if w := lipgloss.Width(line); w > cols {
			cols = w
		}
	}
	box := captchaBoxStyle.Width(cols + 2).Render(rendered)
	centered := lipgloss.PlaceHorizontal(inner, lipgloss.Center, box)
	return strings.Split(centered, "\n")
}

// viewStatusLine renders the one-line status message (busy/error/notice).
func (m loginModel) viewStatusLine(inner int) string {
	var line string
	switch {
	case m.busy:
		line = lipgloss.JoinHorizontal(lipgloss.Center,
			m.spinner.View(), " ", hintStyle.Render(m.busyText))
	case m.errText != "":
		line = errStyle.Render(truncate(m.errText, inner))
	case m.okText != "":
		line = okStyle.Render(truncate(m.okText, inner))
	case m.notice != "":
		line = warnStyle.Render(truncate(m.notice, inner))
	}
	return lipgloss.PlaceHorizontal(inner, lipgloss.Center, line)
}

// viewHints returns the keyboard shortcut hint shown in the bottom bar.
func (m loginModel) viewHints() string {
	if m.focus == focusTabs {
		return "←/→ 选择登录方式 · Tab/↑↓ 切换焦点 · Enter 确认 · Ctrl+C 退出"
	}
	if m.method == methodEmail {
		return "Tab/↑↓ 切换焦点 · Enter 登录 · Ctrl+T 切换登录方式 · Ctrl+C 退出"
	}
	switch m.stage {
	case smsStagePhone:
		return "Enter 获取图形验证码 · Ctrl+T 切换登录方式 · Ctrl+C 退出"
	case smsStageCaptcha:
		return "Enter 发送短信验证码 · Ctrl+R 更换验证码 · Ctrl+S 保存图片 · Esc 返回 · Ctrl+C 退出"
	default:
		return "Enter 登录 · Esc 返回 · Ctrl+C 退出"
	}
}

// ---------------------------------------------------------------------------
// validation helpers

func validateMobile(mobile string) error {
	if len(mobile) != 11 {
		return fmt.Errorf("请输入 11 位手机号")
	}
	for _, r := range mobile {
		if !unicode.IsDigit(r) {
			return fmt.Errorf("手机号只能包含数字")
		}
	}
	if mobile[0] != '1' {
		return fmt.Errorf("手机号格式不正确")
	}
	return nil
}

func validateEmail(email string) error {
	at := strings.LastIndex(email, "@")
	if at <= 0 || at == len(email)-1 {
		return fmt.Errorf("请输入有效的邮箱地址")
	}
	domain := email[at+1:]
	if !strings.Contains(domain, ".") || strings.HasSuffix(domain, ".") {
		return fmt.Errorf("请输入有效的邮箱地址")
	}
	return nil
}

// loginTabAt maps a body-relative column to a login-method tab. It mirrors the
// rendering in viewTabs: two spaces of indent, then the email tab, one space,
// then the SMS tab (plus focus arrows when the tab row itself is focused).
func (m loginModel) loginTabAt(localX int) (loginMethod, bool) {
	indent := 2
	if m.focus == focusTabs {
		indent += 2 // "‹ "
	}
	emailWidth := lipgloss.Width(tabInactiveStyle.Render("邮箱密码"))
	smsWidth := lipgloss.Width(tabInactiveStyle.Render("短信验证码"))
	if localX >= indent && localX < indent+emailWidth {
		return methodEmail, true
	}
	smsStart := indent + emailWidth + 1
	if localX >= smsStart && localX < smsStart+smsWidth {
		return methodSMS, true
	}
	return methodEmail, false
}

// handleMouse implements click interactions on the login screen. It maps the
// clicked row back to the rendered form (tabs, fields and submit button).
func (m loginModel) handleMouse(msg tea.MouseMsg) (loginModel, tea.Cmd) {
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return m, nil
	}
	width, height := screenSize(m.width, m.height)
	inner := width - 2*chromePadX
	avail := maxInt(1, height-4)
	flat := flattenLines(m.bodyLines(inner, avail))
	top := (avail - len(flat)) / 2
	if top < 0 {
		top = 0
	}
	localY := msg.Y - 2 - top
	localX := msg.X - chromePadX
	if localY < 0 || localY >= len(flat) || localX < 0 || localX >= inner {
		return m, nil
	}
	line := flat[localY]

	// First body row is always the login-method tab bar. The tabs are rendered
	// left-aligned (not across the whole row), so hit-test their real widths.
	if localY == 0 {
		if target, ok := m.loginTabAt(localX); ok {
			if target == m.method {
				return m, nil
			}
			m.focus = focusTabs
			key := tea.KeyMsg{Type: tea.KeyRight}
			if target == methodEmail {
				key = tea.KeyMsg{Type: tea.KeyLeft}
			}
			return m.handleKey(key)
		}
		m.focus = focusTabs
		return m, m.applyFocus()
	}

	switch {
	case strings.Contains(line, "图形验证码"):
		m.focus = focusImageCode
		return m, m.applyFocus()
	case strings.Contains(line, "短信验证码"):
		m.focus = focusSMSCode
		return m, m.applyFocus()
	case strings.Contains(line, "邮箱"):
		m.focus = focusEmail
		return m, m.applyFocus()
	case strings.Contains(line, "密码"):
		m.focus = focusPassword
		return m, m.applyFocus()
	case strings.Contains(line, "手机号"):
		m.focus = focusPhone
		return m, m.applyFocus()
	case strings.Contains(line, m.submitLabel()):
		return m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	}
	return m, nil
}
