package main

import (
	"archive/zip"
	"crypto/sha256"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Target struct {
	OS   string
	Arch string
}

func main() {
	// Defaults mirror the previous hardcoded behaviour, so a bare `go run scripts/package.go`
	// still produces the same four archives.
	version := flag.String("version", "1.1.0", "ghost-driver release version stamped into the skill")
	skillTemplate := flag.String("skill", "SKILL_TEMPLATE.md", "client-agnostic skill template rendered to SKILL.md")
	noSkill := flag.Bool("no-skill", false, "do not render SKILL.md into each package")
	targetsFlag := flag.String("targets", "windows/amd64,linux/amd64,darwin/amd64,darwin/arm64", "comma separated os/arch pairs")
	flag.Parse()

	targets := parseTargets(*targetsFlag)

	pkgDir := "packages"
	os.RemoveAll(pkgDir)
	os.MkdirAll(pkgDir, 0755)

	for _, t := range targets {
		platformName := fmt.Sprintf("%s-%s", t.OS, t.Arch)
		fmt.Printf("📦 Packaging Ghost-Driver for %s...\n", platformName)

		// Create target package root
		targetPkgRoot := filepath.Join(pkgDir, platformName)
		os.MkdirAll(targetPkgRoot, 0755)
		os.MkdirAll(filepath.Join(targetPkgRoot, "profiles"), 0755)

		ext := ""
		if t.OS == "windows" {
			ext = ".exe"
		}

		// 1. Build FULL Version (Binary at root for visibility)
		binName := "ghost-driver" + ext
		targetBin := filepath.Join(targetPkgRoot, binName)
		fmt.Printf("  > Building Engine...\n")
		cmd := exec.Command("go", "build", "-ldflags", "-s -w -X main.version="+*version, "-o", targetBin, "./cmd/ghost-driver")
		cmd.Env = append(os.Environ(), "GOOS="+t.OS, "GOARCH="+t.Arch)
		if out, err := cmd.CombinedOutput(); err != nil {
			fmt.Printf("❌ Build failed: %v\n%s", err, string(out))
			continue
		}

		// 2. Copy Skills & Docs
		mustCopy("GHOST_DRIVER_SKILL.md", filepath.Join(targetPkgRoot, "GHOST_DRIVER_SKILL.md"))
		mustCopy("QUICK_START.md", filepath.Join(targetPkgRoot, "QUICK_START.md"))
		mustCopy("USER_GUIDE.md", filepath.Join(targetPkgRoot, "USER_GUIDE.md"))
		mustCopy("USER_GUIDE_EN.md", filepath.Join(targetPkgRoot, "USER_GUIDE_EN.md"))
		mustCopy("README.md", filepath.Join(targetPkgRoot, "README.md"))
		mustCopy("README_EN.md", filepath.Join(targetPkgRoot, "README_EN.md"))
		mustCopy("AGENTS.md", filepath.Join(targetPkgRoot, "AGENTS.md"))

		// 3. Copy ONLY essential Profiles (Agent will do the rest)
		mustCopy("profiles/generic.yaml", filepath.Join(targetPkgRoot, "profiles", "generic.yaml"))

		// 3a. Copy license, notice, and env example for compliance & configuration reference.
		mustCopy("LICENSE", filepath.Join(targetPkgRoot, "LICENSE"))
		mustCopy("NOTICE", filepath.Join(targetPkgRoot, "NOTICE"))
		mustCopy(".env.example", filepath.Join(targetPkgRoot, ".env.example"))

		// 3b. Render the client-agnostic SKILL.md so every host client can consume the package.
		if !*noSkill {
			if err := renderSkill(*skillTemplate, filepath.Join(targetPkgRoot, "SKILL.md"), binName, *version); err != nil {
				fmt.Printf("❌ Skill render failed: %v\n", err)
			} else {
				fmt.Printf("  > SKILL.md rendered (version %s)\n", *version)
			}
		}

		// 4. Create ZIP
		zipFile := filepath.Join(pkgDir, fmt.Sprintf("ghost-driver-v%s-%s.zip", *version, platformName))
		if err := zipSource(targetPkgRoot, zipFile); err != nil {
			fmt.Printf("❌ Zip failed: %v\n", err)
		} else {
			fmt.Printf("✅ Created %s\n", zipFile)
			if err := writeChecksum(zipFile); err != nil {
				fmt.Printf("⚠️ Checksum failed: %v\n", err)
			}
		}

		// Cleanup temp folder
		os.RemoveAll(targetPkgRoot)
	}

	fmt.Println("\n🚀 Distribution build complete. Binary is at root, memory/ folder removed from zip.")
}

// parseTargets accepts "windows/amd64,linux/arm64"; invalid pairs are dropped with a warning.
func parseTargets(spec string) []Target {
	targets := []Target{}
	for _, pair := range strings.Split(spec, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		parts := strings.SplitN(pair, "/", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			fmt.Printf("⚠️ Skipping invalid target %q (expected os/arch)\n", pair)
			continue
		}
		targets = append(targets, Target{OS: parts[0], Arch: parts[1]})
	}
	return targets
}

// renderSkill fills the <<VERSION>> / <<BINARY>> placeholders and writes SKILL.md.
func renderSkill(templatePath, destPath, binName, version string) error {
	data, err := os.ReadFile(templatePath)
	if err != nil {
		return err
	}
	out := strings.NewReplacer(
		"<<VERSION>>", version,
		"<<BINARY>>", binName,
	).Replace(string(data))
	return os.WriteFile(destPath, []byte(out), 0644)
}

// mustCopy fails loudly; the previous copyFile calls discarded the error, so a missing
// source file silently produced an incomplete package.
func mustCopy(src, dst string) {
	if err := copyFile(src, dst); err != nil {
		fmt.Printf("❌ Copy failed %s -> %s: %v\n", src, dst, err)
		os.Exit(1)
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func zipSource(source, target string) error {
	f, err := os.Create(target)
	if err != nil {
		return err
	}
	defer f.Close()
	writer := zip.NewWriter(f)
	defer writer.Close()
	return filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(source, path)
		header.Name = rel
		if info.IsDir() {
			header.Name += "/"
		} else {
			header.Method = zip.Deflate
		}
		writerHeader, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		_, err = io.Copy(writerHeader, file)
		return err
	})
}

// writeChecksum writes a "<hex>  <basename>" SHA256 sidecar file for download verification.
func writeChecksum(file string) error {
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	h := sha256.Sum256(data)
	return os.WriteFile(file+".sha256", []byte(fmt.Sprintf("%x  %s\n", h, filepath.Base(file))), 0644)
}
