package cookie

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// usernameSafe matches valid Windows usernames. Strips path-traversal-capable
// characters so an unusual $USER value can't escape the temp directory.
var usernameSafe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func sanitizeUsername(u string) string {
	u = usernameSafe.ReplaceAllString(u, "")
	if len(u) > 64 {
		u = u[:64]
	}
	return u
}

func ReadBrowserCookies(browser string) ([]*http.Cookie, error) {
	switch browser {
	case "chrome", "edge", "firefox":
	default:
		return nil, fmt.Errorf("unsupported browser: %s (use chrome/edge/firefox)", browser)
	}

	if isWSL() {
		return readCookiesWSL(browser)
	}
	switch runtime.GOOS {
	case "windows":
		return readCookiesWindows(browser)
	case "darwin":
		return readCookiesMacOS(browser)
	case "linux":
		return readCookiesLinux(browser)
	default:
		return nil, fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

func isWSL() bool {
	data, _ := os.ReadFile("/proc/version")
	return strings.Contains(strings.ToLower(string(data)), "microsoft")
}

// readCookiesWSL invokes Windows-side Python to decrypt Chrome/Edge cookies via DPAPI
func readCookiesWSL(browser string) ([]*http.Cookie, error) {
	script := buildExportScript(browser)

	// Write to per-user Windows temp (avoid shared C:\Temp TOCTOU)
	user := sanitizeUsername(os.Getenv("USER"))
	if user == "" {
		user = "default"
	}
	winTmpDir := fmt.Sprintf("/mnt/c/Users/%s/AppData/Local/Temp", user)
	os.MkdirAll(winTmpDir, 0o700)
	scriptPath := filepath.Join(winTmpDir, "medigo_cookie_export.py")
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		return nil, fmt.Errorf("failed to write export script: %w", err)
	}
	defer os.Remove(scriptPath)

	winPath := fmt.Sprintf(`C:\Users\%s\AppData\Local\Temp\medigo_cookie_export.py`, user)
	cmd := exec.Command("python.exe", winPath)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg != "" {
			return nil, fmt.Errorf("cookie export: %s", firstLine(errMsg))
		}
		return nil, fmt.Errorf("Windows Python cookie export failed: %w", err)
	}

	return parseCookieJSON(out)
}

// readCookiesWindows runs the Python script natively on Windows
func readCookiesWindows(browser string) ([]*http.Cookie, error) {
	script := buildExportScript(browser)

	tmpDir := os.TempDir()
	scriptPath := filepath.Join(tmpDir, "medigo_cookie_export.py")
	if err := os.WriteFile(scriptPath, []byte(script), 0o644); err != nil {
		return nil, err
	}
	defer os.Remove(scriptPath)

	cmd := exec.Command("python", scriptPath)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		// Try python3
		cmd = exec.Command("python3", scriptPath)
		cmd.Stderr = &stderr
		out, err = cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("cookie export failed (install Python + pycryptodome): %s", firstLine(stderr.String()))
		}
	}
	return parseCookieJSON(out)
}

// readCookiesMacOS uses security framework keychain + sqlite3
func readCookiesMacOS(browser string) ([]*http.Cookie, error) {
	script := buildMacExportScript(browser)

	scriptPath := filepath.Join(os.TempDir(), "medigo_cookie_export.py")
	if err := os.WriteFile(scriptPath, []byte(script), 0o644); err != nil {
		return nil, err
	}
	defer os.Remove(scriptPath)

	cmd := exec.Command("python3", scriptPath)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("cookie export failed: %s", firstLine(stderr.String()))
	}
	return parseCookieJSON(out)
}

// readCookiesLinux uses secretstorage (GNOME Keyring) for Chrome key
func readCookiesLinux(browser string) ([]*http.Cookie, error) {
	script := buildLinuxExportScript(browser)

	scriptPath := filepath.Join(os.TempDir(), "medigo_cookie_export.py")
	if err := os.WriteFile(scriptPath, []byte(script), 0o644); err != nil {
		return nil, err
	}
	defer os.Remove(scriptPath)

	cmd := exec.Command("python3", scriptPath)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("cookie export failed: %s", firstLine(stderr.String()))
	}
	return parseCookieJSON(out)
}

func parseCookieJSON(data []byte) ([]*http.Cookie, error) {
	var result struct {
		Error   string `json:"error"`
		Count   int    `json:"count"`
		Cookies []struct {
			Host    string `json:"host"`
			Path    string `json:"path"`
			Secure  int    `json:"secure"`
			Expires int64  `json:"expires"`
			Name    string `json:"name"`
			Value   string `json:"value"`
		} `json:"cookies"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse cookie JSON: %w\nraw: %s", err, string(data[:min(200, len(data))]))
	}
	if result.Error != "" {
		return nil, fmt.Errorf("%s", result.Error)
	}

	var cookies []*http.Cookie
	for _, c := range result.Cookies {
		cookies = append(cookies, &http.Cookie{
			Domain: c.Host,
			Path:   c.Path,
			Secure: c.Secure != 0,
			Name:   c.Name,
			Value:  c.Value,
		})
	}
	return cookies, nil
}

func buildExportScript(browser string) string {
	var browserDir string
	switch browser {
	case "edge":
		browserDir = `os.path.expandvars(r"%LOCALAPPDATA%\\Microsoft\\Edge\\User Data")`
	default:
		browserDir = `os.path.expandvars(r"%LOCALAPPDATA%\\Google\\Chrome\\User Data")`
	}

	return fmt.Sprintf(`
import json, base64, sqlite3, os, sys, tempfile, subprocess, ctypes, ctypes.wintypes

class DATA_BLOB(ctypes.Structure):
    _fields_ = [("cbData", ctypes.wintypes.DWORD), ("pbData", ctypes.POINTER(ctypes.c_char))]

def dpapi_decrypt(encrypted):
    blob_in = DATA_BLOB(len(encrypted), ctypes.cast(ctypes.create_string_buffer(encrypted, len(encrypted)), ctypes.POINTER(ctypes.c_char)))
    blob_out = DATA_BLOB()
    if ctypes.windll.crypt32.CryptUnprotectData(ctypes.byref(blob_in), None, None, None, None, 0, ctypes.byref(blob_out)):
        result = ctypes.string_at(blob_out.pbData, blob_out.cbData)
        ctypes.windll.kernel32.LocalFree(blob_out.pbData)
        return result
    return None

BROWSER_DIR = %s
local_state_path = os.path.join(BROWSER_DIR, "Local State")
cookie_db_path = os.path.join(BROWSER_DIR, r"Default\Network\Cookies")

with open(local_state_path, 'r', encoding='utf-8') as f:
    local_state = json.load(f)
key_b64 = local_state['os_crypt']['encrypted_key']
encrypted_key = base64.b64decode(key_b64)[5:]
aes_key = dpapi_decrypt(encrypted_key)
if not aes_key:
    print(json.dumps({"error": "DPAPI decrypt failed - run as the same user who owns Chrome"}))
    sys.exit(0)

tmp = tempfile.mktemp(suffix='.db')
src_dir = os.path.dirname(cookie_db_path)
src_name = os.path.basename(cookie_db_path)
tmp_dir = os.path.dirname(tmp)
r = subprocess.run(['robocopy', src_dir, tmp_dir, src_name, '/NFL', '/NDL', '/NJH', '/NJS', '/NC', '/NS'], capture_output=True)
copied = os.path.join(tmp_dir, src_name)
if not os.path.exists(copied) or os.path.getsize(copied) == 0:
    # Try direct copy (Chrome must be closed)
    try:
        import shutil
        shutil.copy2(cookie_db_path, tmp)
    except:
        print(json.dumps({"error": "Cannot read Chrome cookies while Chrome is running. Close Chrome and retry, or use --cookies with a Netscape cookie file."}))
        sys.exit(0)
else:
    os.rename(copied, tmp)

try:
    from Crypto.Cipher import AES
except ImportError:
    try:
        from Cryptodome.Cipher import AES
    except ImportError:
        print(json.dumps({"error": "pycryptodome not installed. Run: pip install pycryptodome"}))
        sys.exit(0)

conn = sqlite3.connect(tmp)
c = conn.cursor()
try:
    c.execute('SELECT host_key, path, is_secure, expires_utc, name, encrypted_value FROM cookies')
except Exception as e:
    os.remove(tmp)
    print(json.dumps({"error": f"Cannot read cookies table: {e}. Chrome DB may be empty while running."}))
    sys.exit(0)

cookies = []
for host, path, secure, expires, name, enc_value in c.fetchall():
    if not enc_value:
        continue
    try:
        if enc_value[:3] in (b'v10', b'v20'):
            nonce = enc_value[3:15]
            ciphertext = enc_value[15:-16]
            tag = enc_value[-16:]
            cipher = AES.new(aes_key, AES.MODE_GCM, nonce=nonce)
            raw = cipher.decrypt_and_verify(ciphertext, tag)
            # Chrome 127+ ABE: strip binary header, find first printable ASCII run
            value = None
            for offset in range(len(raw)):
                candidate = raw[offset:]
                if len(candidate) > 0 and all(32 <= b < 127 or b == 9 for b in candidate):
                    value = candidate.decode('ascii')
                    break
            if not value:
                continue
        else:
            decrypted = dpapi_decrypt(enc_value)
            if not decrypted:
                continue
            value = decrypted.decode('latin-1')
    except:
        continue
    if not value:
        continue
    unix_expires = 0
    if expires > 0:
        unix_expires = (expires - 11644473600000000) // 1000000
    cookies.append({"host": host, "path": path, "secure": 1 if secure else 0, "expires": unix_expires, "name": name, "value": value})

conn.close()
os.remove(tmp)
print(json.dumps({"cookies": cookies, "count": len(cookies)}))
`, browserDir)
}

func buildMacExportScript(browser string) string {
	var dbPath, serviceName string
	switch browser {
	case "edge":
		dbPath = `os.path.expanduser("~/Library/Application Support/Microsoft Edge/Default/Cookies")`
		serviceName = "Microsoft Edge Safe Storage"
	default:
		dbPath = `os.path.expanduser("~/Library/Application Support/Google/Chrome/Default/Cookies")`
		serviceName = "Chrome Safe Storage"
	}

	return fmt.Sprintf(`
import json, sqlite3, os, sys, subprocess, hashlib, tempfile, shutil

db_path = %s
service_name = "%s"

# Get encryption key from Keychain
result = subprocess.run(['security', 'find-generic-password', '-s', service_name, '-w'], capture_output=True, text=True)
if result.returncode != 0:
    print(json.dumps({"error": f"Cannot access Keychain for {service_name}"}))
    sys.exit(0)

password = result.stdout.strip().encode()
key = hashlib.pbkdf2_hmac('sha1', password, b'saltysalt', 1003, dklen=16)

tmp = tempfile.mktemp(suffix='.db')
shutil.copy2(db_path, tmp)

from Crypto.Cipher import AES

conn = sqlite3.connect(tmp)
c = conn.cursor()
c.execute('SELECT host_key, path, is_secure, expires_utc, name, encrypted_value FROM cookies')

cookies = []
for host, path, secure, expires, name, enc_value in c.fetchall():
    if not enc_value:
        continue
    try:
        if enc_value[:3] == b'v10':
            enc_value = enc_value[3:]
            iv = b' ' * 16
            cipher = AES.new(key, AES.MODE_CBC, IV=iv)
            value = cipher.decrypt(enc_value)
            padding = value[-1]
            value = value[:-padding].decode('utf-8', errors='replace')
        else:
            value = enc_value.decode('utf-8', errors='replace')
    except:
        continue
    if not value:
        continue
    unix_expires = 0
    if expires > 0:
        unix_expires = (expires - 11644473600000000) // 1000000
    cookies.append({"host": host, "path": path, "secure": 1 if secure else 0, "expires": unix_expires, "name": name, "value": value})

conn.close()
os.remove(tmp)
print(json.dumps({"cookies": cookies, "count": len(cookies)}))
`, dbPath, serviceName)
}

func buildLinuxExportScript(browser string) string {
	var dbPath string
	switch browser {
	case "edge":
		dbPath = `os.path.expanduser("~/.config/microsoft-edge/Default/Cookies")`
	default:
		dbPath = `os.path.expanduser("~/.config/google-chrome/Default/Cookies")`
	}

	return fmt.Sprintf(`
import json, sqlite3, os, sys, hashlib, tempfile, shutil

db_path = %s

# Try to get key from GNOME Keyring via secretstorage
key = None
try:
    import secretstorage
    bus = secretstorage.dbus_init()
    collection = secretstorage.get_default_collection(bus)
    for item in collection.get_all_items():
        if item.get_label() == 'Chrome Safe Storage':
            key = item.get_secret()
            break
except:
    pass

if key is None:
    # Fallback: hardcoded default key used when no keyring
    key = b'peanuts'

derived_key = hashlib.pbkdf2_hmac('sha1', key, b'saltysalt', 1, dklen=16)

tmp = tempfile.mktemp(suffix='.db')
shutil.copy2(db_path, tmp)

from Crypto.Cipher import AES

conn = sqlite3.connect(tmp)
c = conn.cursor()
c.execute('SELECT host_key, path, is_secure, expires_utc, name, encrypted_value FROM cookies')

cookies = []
for host, path, secure, expires, name, enc_value in c.fetchall():
    if not enc_value:
        continue
    try:
        if enc_value[:3] == b'v11' or enc_value[:3] == b'v10':
            enc_value = enc_value[3:]
            iv = b' ' * 16
            cipher = AES.new(derived_key, AES.MODE_CBC, IV=iv)
            value = cipher.decrypt(enc_value)
            padding = value[-1]
            value = value[:-padding].decode('utf-8', errors='replace')
        else:
            value = enc_value.decode('utf-8', errors='replace')
    except:
        continue
    if not value:
        continue
    unix_expires = 0
    if expires > 0:
        unix_expires = (expires - 11644473600000000) // 1000000
    cookies.append({"host": host, "path": path, "secure": 1 if secure else 0, "expires": unix_expires, "name": name, "value": value})

conn.close()
os.remove(tmp)
print(json.dumps({"cookies": cookies, "count": len(cookies)}))
`, dbPath)
}

func firstLine(s string) string {
	if idx := strings.Index(s, "\n"); idx > 0 {
		return s[:idx]
	}
	return s
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
