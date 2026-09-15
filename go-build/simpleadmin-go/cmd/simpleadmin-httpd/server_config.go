package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func validateStaticDir(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", path)
	}
	indexPath := filepath.Join(path, "index.html")
	if _, err := os.Stat(indexPath); err != nil {
		return fmt.Errorf("index.html missing in %s: %w", path, err)
	}
	return nil
}

func ensureAuthFile(path string) error {
	info, err := os.Stat(path)
	if err == nil && info.Size() > 0 {
		if _, loadErr := loadAuthConfig(path); loadErr == nil {
			return nil
		}
		return resetCorruptAuthFile(path)
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte("admin:admin\n"), 0600)
}

// resetCorruptAuthFile moves an unparseable auth file to
// <name>.corrupt-<timestamp> and rewrites it with the default credentials so
// login and password change are never silently locked out. The backup path is
// logged so the original content can be recovered.
func resetCorruptAuthFile(path string) error {
	backupPath := fmt.Sprintf("%s.corrupt-%s", path, time.Now().Format("20060102-150405"))
	if err := os.Rename(path, backupPath); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte("admin:admin\n"), 0600); err != nil {
		return err
	}
	log.Printf("警告: 认证文件无法解析,已备份为 %s 并重置为默认凭据,请尽快登录后修改密码", backupPath)
	return nil
}

func loadAuthConfig(path string) (authConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return authConfig{}, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 || parts[0] == "" {
			return authConfig{}, fmt.Errorf("认证文件格式错误: %s", path)
		}
		// 空密码等价于零口令认证(登录侧 constantTimeEqual("","") 恒真):
		// 手工写出的 "admin:" 或纯空白密码行(整行 TrimSpace 后即此形态)
		// 必须判为不可解析,由 ensureAuthFile 走损坏自愈(备份+重置默认
		// 凭据+告警),而不是静默放行任何人以空密码登录。
		if parts[1] == "" {
			return authConfig{}, fmt.Errorf("认证文件密码为空: %s", path)
		}
		return authConfig{Username: parts[0], Password: parts[1]}, nil
	}
	return authConfig{}, fmt.Errorf("认证文件为空: %s", path)
}

func writeAuthConfig(path string, auth authConfig) error {
	if strings.TrimSpace(auth.Username) == "" {
		return errors.New("username is empty")
	}
	if err := validateNewPassword(auth.Password); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".simpleadmin.auth.*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := fmt.Fprintf(tmp, "%s:%s\n", auth.Username, auth.Password); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return err
	}
	// rename 前先落盘(与其余原子写一致):设备掉电常见,避免 rename 后
	// 元数据已更新而数据仍在页缓存,掉电留下截断的认证文件触发重置。
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func validateNewPassword(password string) error {
	if password == "" {
		return errors.New("new password is empty")
	}
	if strings.ContainsAny(password, "\r\n") {
		return errors.New("new password must not contain line breaks")
	}
	// 读写必须对称:loadAuthConfig 对整行 TrimSpace,带首尾空白的密码写入
	// 后读回即被裁掉——用户设置的 "abc " 静默变异为 "abc",按自己设的
	// 密码登录恒 401 还会撞上失败锁定;纯空白密码则退化为空密码。
	if strings.TrimSpace(password) != password {
		return errors.New("new password must not have leading or trailing whitespace")
	}
	if len(password) > 128 {
		return errors.New("new password is too long")
	}
	return nil
}
func (s *simpleAdminServer) languageConfigPath() string {
	return filepath.Join(s.cfg.staticDir, languageConfigRelPath)
}

func normalizeLanguage(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "zh", "zh-cn", "cn", "chinese":
		return "zh-CN"
	case "en", "en-us", "english":
		return "en"
	default:
		return ""
	}
}

func readLanguageConfig(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return defaultLanguage, err
	}

	var cfg languageConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return defaultLanguage, err
	}
	if language := normalizeLanguage(cfg.Language); language != "" {
		return language, nil
	}
	return defaultLanguage, nil
}

// writeLanguageConfig 原子写入语言配置(临时文件 → Sync → rename),
// 与 writeThemeConfig 一致:直接覆写遇掉电会留下半截 JSON,
// 读取解析失败后静默回退缺省语言,用户保存的语言设置无提示丢失。
func writeLanguageConfig(path, language string) error {
	language = normalizeLanguage(language)
	if language == "" {
		return fmt.Errorf("unsupported language: %s", language)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(languageConfig{Language: language}, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(path), ".simpleadmin.lang.*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0644); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
func (s *simpleAdminServer) handleGetLanguage(w http.ResponseWriter, r *http.Request) {
	language, err := readLanguageConfig(s.languageConfigPath())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("读取语言配置失败: %v", err)
	}
	writeJSON(w, http.StatusOK, languageConfig{Language: language})
}

func (s *simpleAdminServer) handleSetLanguage(w http.ResponseWriter, r *http.Request) {
	language := normalizeLanguage(r.URL.Query().Get("language"))
	if language == "" && r.Method == http.MethodPost {
		var cfg languageConfig
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&cfg); err == nil {
			language = normalizeLanguage(cfg.Language)
		}
	}
	if language == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported language"})
		return
	}
	if err := writeLanguageConfig(s.languageConfigPath(), language); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, languageConfig{Language: language})
}

func (s *simpleAdminServer) themeConfigPath() string {
	return filepath.Join(s.cfg.staticDir, themeConfigRelPath)
}

func normalizeTheme(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "dark":
		return "dark"
	case "light":
		return "light"
	default:
		return ""
	}
}

func readThemeConfig(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return defaultTheme, err
	}

	var cfg themeConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return defaultTheme, err
	}
	if theme := normalizeTheme(cfg.Theme); theme != "" {
		return theme, nil
	}
	return defaultTheme, nil
}

// writeThemeConfig 原子写入主题配置(临时文件 → Sync → rename),
// 避免服务异常时留下半截 JSON 导致默认主题读取失败。
func writeThemeConfig(path, theme string) error {
	theme = normalizeTheme(theme)
	if theme == "" {
		return fmt.Errorf("unsupported theme: %s", theme)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(themeConfig{Theme: theme}, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(path), ".simpleadmin.theme.*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0644); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func (s *simpleAdminServer) handleGetTheme(w http.ResponseWriter, r *http.Request) {
	theme, err := readThemeConfig(s.themeConfigPath())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("读取主题配置失败: %v", err)
	}
	writeJSON(w, http.StatusOK, themeConfig{Theme: theme})
}

func (s *simpleAdminServer) handleSetTheme(w http.ResponseWriter, r *http.Request) {
	theme := normalizeTheme(r.URL.Query().Get("theme"))
	if theme == "" && r.Method == http.MethodPost {
		var cfg themeConfig
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&cfg); err == nil {
			theme = normalizeTheme(cfg.Theme)
		}
	}
	if theme == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported theme"})
		return
	}
	if err := writeThemeConfig(s.themeConfigPath(), theme); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, themeConfig{Theme: theme})
}

func (s *simpleAdminServer) handleSetPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	// 当前密码校验复用登录限流:此处曾是同口令爆破的旁路——登录侧 5 次
	// 即锁定,而被窃会话(HTTP-only 部署下 Cookie 明文过 LAN,本模块
	// destroyOtherSessions 注释自认的威胁模型)可在这里无限速猜当前密码,
	// 命中即改密+吊销其余会话完成账户接管,登录限流的防护价值被清零。
	remoteIP := loginRemoteIP(r)
	if remaining := s.loginLockedRemaining(remoteIP); remaining > 0 {
		retryAfter := int((remaining + time.Second - 1) / time.Second)
		w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"ok": false, "error": "too many failed attempts", "retry_after": retryAfter})
		return
	}

	auth, err := loadAuthConfig(s.cfg.authFile)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "auth config error"})
		return
	}

	currentPassword := requestValue(r, "current_password")
	if currentPassword == "" {
		currentPassword = requestValue(r, "currentPassword")
	}
	newPassword := requestValue(r, "new_password")
	if newPassword == "" {
		newPassword = requestValue(r, "newPassword")
	}
	confirmPassword := requestValue(r, "confirm_password")
	if confirmPassword == "" {
		confirmPassword = requestValue(r, "confirmPassword")
	}

	if !constantTimeEqual(currentPassword, auth.Password) {
		s.recordLoginFailure(remoteIP)
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "current password incorrect"})
		return
	}
	// 当前密码验证通过即证明口令知识,与登录成功同口径清零失败计数。
	s.resetLoginAttempts(remoteIP)
	if confirmPassword != "" && newPassword != confirmPassword {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "password confirmation mismatch"})
		return
	}
	if err := validateNewPassword(newPassword); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := writeAuthConfig(s.cfg.authFile, authConfig{Username: auth.Username, Password: newPassword}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if _, err := loadAuthConfig(s.cfg.authFile); err != nil {
		if resetErr := resetCorruptAuthFile(s.cfg.authFile); resetErr != nil {
			log.Printf("警告: 重置损坏的认证文件失败: %v", resetErr)
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "auth config error"})
		return
	}

	// 改密成功即吊销其余全部会话:内存令牌不因密码变更自动失效,不吊销则
	// 被窃令牌在改密后仍可持续使用(每请求滑动续期,永不过期),管理员
	// 改密止损形同虚设。当前会话保留,操作端无需重新登录。
	s.destroyOtherSessions(r)

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
