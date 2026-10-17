package acceptance_test

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	"github.com/xiaoxuan010/napcat_exporter/internal/config"
	"github.com/xiaoxuan010/napcat_exporter/internal/contract"
)

func TestPackageLifecycle(t *testing.T) {
	archive := os.Getenv("NAPCAT_DEB")
	if archive == "" {
		t.Skip("set NAPCAT_DEB in an isolated root/systemd environment")
	}
	if os.Geteuid() != 0 {
		t.Fatal("package acceptance requires root in an isolated environment")
	}
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		t.Fatal("package acceptance requires running systemd")
	}
	packageName := command(t, "dpkg-deb", "--field", archive, "Package")
	if exec.Command("dpkg-query", "--show", packageName).Run() == nil {
		t.Fatal("refusing to modify an existing package installation")
	}
	// Repack the actual artifact with an older version to exercise dpkg upgrade hooks.
	previous := filepath.Join(t.TempDir(), "previous")
	command(t, "dpkg-deb", "--raw-extract", archive, previous)
	controlPath := filepath.Join(previous, "DEBIAN", "control")
	control, err := os.ReadFile(controlPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(control), "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "Version:") {
			lines[i] = "Version: 0.0.0~acceptance"
		}
	}
	if err := os.WriteFile(controlPath, []byte(strings.Join(lines, "\n")), 0644); err != nil {
		t.Fatal(err)
	}
	oldArchive := filepath.Join(t.TempDir(), "previous.deb")
	command(t, "dpkg-deb", "--build", previous, oldArchive)
	t.Cleanup(func() {
		output, err := exec.Command("dpkg", "--purge", packageName).CombinedOutput()
		if err != nil {
			t.Errorf("package cleanup: %v\n%s", err, output)
		}
	})
	command(t, "dpkg", "--install", oldArchive)
	var unit, envFile string
	for _, file := range strings.Split(command(t, "dpkg-query", "--listfiles", packageName), "\n") {
		if strings.HasSuffix(file, ".service") {
			if unit != "" {
				t.Fatal("multiple service units")
			}
			unit = filepath.Base(file)
		}
	}
	for _, line := range strings.Split(command(t, "dpkg-query", "--show", "--showformat=${Conffiles}", packageName), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && strings.HasPrefix(fields[0], "/etc/") {
			if envFile != "" {
				t.Fatal("multiple package config files")
			}
			envFile = fields[0]
		}
	}
	if unit == "" || envFile == "" {
		t.Fatal("missing service or config in package metadata")
	}
	inactive := func() {
		if exec.Command("systemctl", "--quiet", "is-active", unit).Run() == nil || exec.Command("systemctl", "--quiet", "is-enabled", unit).Run() == nil {
			t.Fatal("installation enabled or started a disabled service")
		}
	}
	inactive()
	token := rand.Text()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{contract.FieldStatus: contract.StatusOK, contract.FieldRetcode: contract.RetcodeOK, contract.FieldData: map[string]any{contract.FieldOnline: true}})
	}))
	defer upstream.Close()
	address := freeAddress(t)
	metricsPath := "/" + rand.Text()
	configuration := fmt.Sprintf("%s=%s\n%s=%s\n%s=%s\n%s=%s\n", envName(config.APIURL), upstream.URL, envName(config.Token), token, envName(config.Listen), address, envName(config.MetricsPath), metricsPath)
	if err := os.WriteFile(envFile, []byte(configuration), 0600); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: time.Second}
	checkService := func() {
		waitFor(t, "installed exporter health", func() bool {
			resp, err := client.Get("http://" + address + contract.HealthPath)
			if err != nil {
				return false
			}
			_ = resp.Body.Close()
			return resp.StatusCode == http.StatusOK
		})
		pid := command(t, "systemctl", "show", unit, "--property=MainPID", "--value")
		if pid == "0" {
			t.Fatal("missing service process")
		}
		status, err := os.ReadFile(filepath.Join("/proc", pid, "status"))
		if err != nil {
			t.Fatal(err)
		}
		uidFound := false
		for _, line := range strings.Split(string(status), "\n") {
			if strings.HasPrefix(line, "Uid:") {
				uidFound = true
				for _, field := range strings.Fields(line)[1:] {
					uid, err := strconv.Atoi(field)
					if err != nil || uid == 0 {
						t.Fatal("service is privileged")
					}
				}
			}
		}
		if !uidFound {
			t.Fatal("cannot inspect process identity")
		}
		resp, err := client.Get("http://" + address + metricsPath)
		if err != nil {
			t.Fatal(err)
		}
		parser := expfmt.NewTextParser(model.LegacyValidation)
		metrics, parseErr := parser.TextToMetricFamilies(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatal("installed metrics endpoint failed")
		}
		api := metrics[contract.APIUp]
		if parseErr != nil || api == nil || len(api.Metric) != 1 || api.Metric[0].GetGauge().GetValue() != contract.Up {
			t.Fatal("installed service configuration or upstream authentication did not take effect")
		}
	}
	command(t, "systemctl", "enable", "--now", unit)
	checkService()
	command(t, "dpkg", "--install", archive)
	checkService()
	actual, err := os.ReadFile(envFile)
	if err != nil || string(actual) != configuration {
		t.Fatal("upgrade did not preserve local configuration")
	}
	command(t, "systemctl", "disable", "--now", unit)
	command(t, "dpkg", "--install", archive)
	inactive()
	actual, err = os.ReadFile(envFile)
	if err != nil || string(actual) != configuration {
		t.Fatal("reinstall changed configuration")
	}
	command(t, "systemctl", "start", unit)
	checkService()
	command(t, "dpkg", "--remove", packageName)
	if exec.Command("systemctl", "--quiet", "is-active", unit).Run() == nil {
		t.Fatal("remove left service running")
	}
	actual, err = os.ReadFile(envFile)
	if err != nil || string(actual) != configuration {
		t.Fatal("remove lost local configuration")
	}
	command(t, "dpkg", "--purge", packageName)
	if _, err := os.Stat(envFile); !os.IsNotExist(err) {
		t.Fatal("purge left package-owned configuration")
	}
}
