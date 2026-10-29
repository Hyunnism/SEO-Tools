package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode"
)

func TestScheduling_FilesExistAndNoSecrets(t *testing.T) {
	// Menemukan path root proyek relatif terhadap paket internal/cli
	projectRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("gagal menentukan project root: %v", err)
	}

	schedDir := filepath.Join(projectRoot, "examples", "scheduling")
	requiredFiles := []string{
		"setup_task_windows.ps1",
		"crontab.example",
		"post_deploy_hook.ps1",
		"post_deploy_hook.sh",
	}

	for _, filename := range requiredFiles {
		fullPath := filepath.Join(schedDir, filename)
		data, err := os.ReadFile(fullPath)
		if err != nil {
			t.Errorf("berkas contoh otomasi %s tidak ditemukan: %v", filename, err)
			continue
		}

		content := string(data)

		// 1. Verifikasi tidak ada rahasia atau kunci privat yang bocor
		suspiciousWords := []string{
			"BEGIN PRIVATE KEY",
			"client_secret",
			"bot_token",
			"bearer ",
		}
		for _, w := range suspiciousWords {
			if strings.Contains(strings.ToLower(content), strings.ToLower(w)) {
				t.Errorf("ditemukan pola kredensial mencurigakan %q pada berkas %s", w, filename)
			}
		}

		// 2. Verifikasi tidak ada karakter emoji atau non-ASCII
		for i, r := range content {
			if r > unicode.MaxASCII {
				t.Errorf("ditemukan karakter non-ASCII %q pada offset %d di berkas %s (wajib 100%% ASCII)", r, i, filename)
				break
			}
		}
	}
}

func TestScheduling_CrontabSyntax(t *testing.T) {
	projectRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("gagal menentukan project root: %v", err)
	}

	cronPath := filepath.Join(projectRoot, "examples", "scheduling", "crontab.example")
	data, err := os.ReadFile(cronPath)
	if err != nil {
		t.Fatalf("gagal membaca crontab.example: %v", err)
	}

	lines := strings.Split(string(data), "\n")
	foundActiveJob := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		fields := strings.Fields(trimmed)
		if len(fields) < 6 {
			t.Errorf("baris cron tidak valid (kurang dari 6 elemen): %s", trimmed)
			continue
		}

		// Periksa format 5 kolom waktu: menit jam dom month dow
		minute, hour, dom, month, dow := fields[0], fields[1], fields[2], fields[3], fields[4]
		if minute != "0" || hour != "7" || dom != "*" || month != "*" || dow != "1" {
			t.Errorf("jadwal cron mingguan tidak sesuai spesifikasi (harus 0 7 * * 1): %s", trimmed)
		}

		if !strings.Contains(trimmed, "seo-boost run") {
			t.Errorf("baris cron tidak memanggil perintah seo-boost run: %s", trimmed)
		}
		foundActiveJob = true
	}

	if !foundActiveJob {
		t.Errorf("tidak ditemukan baris job cron aktif di crontab.example")
	}
}

func TestScheduling_WindowsTaskScriptWhatIf(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("pengujian PowerShell WhatIf hanya dijalankan di OS Windows")
	}

	projectRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("gagal menentukan project root: %v", err)
	}

	scriptPath := filepath.Join(projectRoot, "examples", "scheduling", "setup_task_windows.ps1")

	cmd := exec.Command("powershell", "-ExecutionPolicy", "Bypass", "-File", scriptPath, "-WhatIf")
	out, err := cmd.CombinedOutput()
	outStr := string(out)

	if err != nil {
		t.Fatalf("eksekusi setup_task_windows.ps1 -WhatIf gagal: %v\nOutput: %s", err, outStr)
	}

	if !strings.Contains(outStr, "[WHATIF]") {
		t.Errorf("output tidak memuat penanda [WHATIF]: %s", outStr)
	}
	if !strings.Contains(outStr, "schtasks.exe /Create /SC WEEKLY /D MON /ST 07:00 /TN SEOBoost_WeeklyAudit") {
		t.Errorf("output tidak memuat perintah schtasks yang diharapkan: %s", outStr)
	}
	if !strings.Contains(outStr, "[OK] Simulasi selesai") {
		t.Errorf("output tidak memuat konfirmasi sukses simulasi: %s", outStr)
	}
}
