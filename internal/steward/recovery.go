package steward

import (
	"bufio"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	routesteward "github.com/squarepots/route-steward"
)

type recoveryMetadata struct {
	Schema          int    `json:"schema"`
	CreatedAt       string `json:"created_at"`
	Product         string `json:"product"`
	ProductVersion  string `json:"product_version"`
	Repository      string `json:"repository,omitempty"`
	Commit          string `json:"commit,omitempty"`
	InventorySchema int    `json:"inventory_schema"`
	RecoveryModel   string `json:"recovery_model"`
}

func newRecoveryMetadata() recoveryMetadata {
	return recoveryMetadata{
		Schema:          1,
		CreatedAt:       utcNow(),
		Product:         "route-steward",
		ProductVersion:  routesteward.Version(),
		Repository:      "https://github.com/squarepots/route-steward",
		InventorySchema: InventorySchema,
		RecoveryModel:   "agent-native-local-state",
	}
}

func CreateRecoveryArchive(state *State, sevenZipPath string) (string, error) {
	sevenZip, err := resolveSevenZip(sevenZipPath)
	if err != nil {
		return "", err
	}
	recoveryDir := state.Inventory.Delivery.RecoveryDirectory
	if recoveryDir == "" {
		recoveryDir = filepath.Join(state.PrivateDir, "recovery")
	}
	if err := os.MkdirAll(recoveryDir, 0o700); err != nil {
		return "", err
	}
	if err := protectPath(recoveryDir, true); err != nil {
		return "", err
	}
	archive := filepath.Join(recoveryDir, "route-steward-recovery-"+timeStamp()+".7z")
	stage, err := os.MkdirTemp("", "route-steward-recovery-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)
	if err := protectPath(stage, true); err != nil {
		return "", err
	}
	copyOne := func(source, relative string) error {
		return copyRegularFile(source, filepath.Join(stage, filepath.FromSlash(relative)))
	}
	if err := writeJSONAtomic(filepath.Join(stage, "private", "inventory.json"), state.Inventory); err != nil {
		return "", err
	}
	if err := copyTree(filepath.Join(state.PrivateDir, "secrets"), filepath.Join(stage, "private", "secrets")); err != nil {
		return "", err
	}
	var migrations *migrationState
	migrationPath := filepath.Join(state.PrivateDir, migrationStateFile)
	if regularFile(migrationPath) {
		migrations, err = readMigrationStateFile(migrationPath)
		if err != nil {
			return "", err
		}
		if err := copyOne(migrationPath, filepath.ToSlash(filepath.Join("private", migrationStateFile))); err != nil {
			return "", err
		}
	}
	archivedSSH := map[string]bool{}
	for _, server := range state.Inventory.Servers {
		if err := copyOne(server.SSH.KeyPath, filepath.ToSlash(filepath.Join("ssh", server.ID, filepath.Base(server.SSH.KeyPath)))); err != nil {
			return "", err
		}
		archivedSSH[server.ID] = true
	}
	if migrations != nil {
		for _, txn := range migrations.Transactions {
			if archivedSSH[txn.ReplacementServer] || txn.ReplacementServerContext == nil {
				continue
			}
			keyPath := stringField(txn.ReplacementServerContext, "ssh_key_path")
			if !regularFile(keyPath) {
				return "", errors.New("active migration replacement SSH key is unavailable for recovery")
			}
			if err := copyOne(keyPath, filepath.ToSlash(filepath.Join("ssh", txn.ReplacementServer, filepath.Base(keyPath)))); err != nil {
				return "", err
			}
			archivedSSH[txn.ReplacementServer] = true
		}
	}
	metadata := newRecoveryMetadata()
	if err := writeJSONAtomic(filepath.Join(stage, "RECOVERY-METADATA.json"), metadata); err != nil {
		return "", err
	}
	startHere := "# Route Steward recovery\n\nGive this encrypted recovery archive and the Route Steward repository to a capable AI agent and ask it to recover local state before auditing existing Routes.\n\nThe archive contains live proxy credentials and SSH private keys. Enter the password only into the local 7-Zip prompt opened by Route Steward. Do not paste it into an AI conversation, process argument, repository file, or log.\n\nRestored desired state and secrets are canonical. Observed state is reset because old observations do not prove that remote infrastructure still matches.\n"
	if err := writeFileAtomic(filepath.Join(stage, "START-HERE.md"), []byte(startHere), 0o600); err != nil {
		return "", err
	}
	if err := writeRecoveryManifest(stage); err != nil {
		return "", err
	}
	if err := protectTree(stage); err != nil {
		return "", err
	}
	fmt.Fprintln(os.Stderr, "7-Zip will request the recovery password locally. The password is not passed as a command argument or logged.")
	if err := runInteractive(sevenZip, "a", "-t7z", "-mhe=on", "-mx=7", "-p", "--", archive, filepath.Join(stage, "*")); err != nil {
		_ = os.Remove(archive)
		return "", errors.New("encrypted recovery archive creation failed")
	}
	if !regularFile(archive) {
		return "", errors.New("encrypted recovery archive was not created")
	}
	if err := protectPath(archive, false); err != nil {
		return "", err
	}
	fmt.Fprintln(os.Stderr, "Re-enter the password locally so 7-Zip can verify the encrypted archive.")
	if err := runInteractive(sevenZip, "t", "-p", "--", archive); err != nil {
		_ = os.Remove(archive)
		return "", errors.New("encrypted recovery archive verification failed")
	}
	return archive, nil
}

func RestoreRecoveryArchive(archive, destination, sevenZipPath string) (map[string]any, error) {
	sevenZip, err := resolveSevenZip(sevenZipPath)
	if err != nil {
		return nil, err
	}
	archive, err = filepath.Abs(archive)
	if err != nil || !regularFile(archive) {
		return nil, errors.New("recovery archive is missing")
	}
	destination, err = filepath.Abs(destination)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(destination); err == nil {
		return nil, errors.New("recovery destination already exists")
	}
	extract, err := os.MkdirTemp("", "rst-recovery-extract-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(extract)
	if err := protectPath(extract, true); err != nil {
		return nil, err
	}
	fmt.Fprintln(os.Stderr, "7-Zip will request the recovery password locally. Do not paste it into an AI conversation.")
	if err := runInteractive(sevenZip, "x", "-p", "-o"+extract, "-y", "--", archive); err != nil {
		return nil, errors.New("recovery archive extraction failed")
	}
	if err := protectTree(extract); err != nil {
		return nil, err
	}
	return RestoreExtractedRecovery(extract, destination)
}

func RestoreExtractedRecovery(sourceRoot, target string) (result map[string]any, err error) {
	sourceRoot, err = filepath.Abs(sourceRoot)
	if err != nil {
		return nil, err
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(target); err == nil {
		return nil, errors.New("recovery destination already exists")
	}
	if err := filepath.WalkDir(sourceRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("recovery archive contains symbolic links or reparse points")
		}
		return nil
	}); err != nil {
		return nil, err
	}
	verified, err := verifyRecoveryManifest(sourceRoot)
	if err != nil {
		return nil, err
	}
	var metadata recoveryMetadata
	if err := readJSON(filepath.Join(sourceRoot, "RECOVERY-METADATA.json"), &metadata); err != nil {
		return nil, errors.New("recovery archive lacks compatible metadata")
	}
	if metadata.Product != "route-steward" || (metadata.InventorySchema != 1 && metadata.InventorySchema != InventorySchema) {
		return nil, errors.New("recovery archive metadata is incompatible")
	}
	var inventory Inventory
	if err := readJSON(filepath.Join(sourceRoot, "private", "inventory.json"), &inventory); err != nil {
		return nil, errors.New("recovery archive lacks an inventory")
	}
	if inventory.Schema != InventorySchema {
		return nil, errors.New("recovered inventory schema is unsupported")
	}
	if !regularFile(filepath.Join(sourceRoot, "private", "secrets", "index.json")) {
		return nil, errors.New("recovery archive does not contain complete canonical state")
	}
	var migrations *migrationState
	migrationSource := filepath.Join(sourceRoot, "private", migrationStateFile)
	if regularFile(migrationSource) {
		migrations, err = readMigrationStateFile(migrationSource)
		if err != nil {
			return nil, err
		}
	}
	parent := filepath.Dir(target)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return nil, err
	}
	stage, err := os.MkdirTemp(parent, ".rst-restore-*")
	if err != nil {
		return nil, err
	}
	moved, completed := false, false
	defer func() {
		if !moved {
			_ = os.RemoveAll(stage)
		} else if !completed {
			_ = os.RemoveAll(target)
		}
	}()
	if err := copyTree(filepath.Join(sourceRoot, "private", "secrets"), filepath.Join(stage, "secrets")); err != nil {
		return nil, err
	}
	for _, directory := range []string{"ssh", "delivery", "recovery"} {
		if err := os.MkdirAll(filepath.Join(stage, directory), 0o700); err != nil {
			return nil, err
		}
	}
	for i := range inventory.Servers {
		server := &inventory.Servers[i]
		sourceDir := filepath.Join(sourceRoot, "ssh", server.ID)
		entries, err := os.ReadDir(sourceDir)
		if err != nil {
			return nil, fmt.Errorf("recovery archive lacks SSH key for Server %q", server.ID)
		}
		files := []string{}
		for _, entry := range entries {
			if entry.Type().IsRegular() {
				files = append(files, entry.Name())
			}
		}
		if len(files) != 1 {
			return nil, fmt.Errorf("recovery archive must contain exactly one SSH private key for Server %q", server.ID)
		}
		destinationRelative := filepath.Join("ssh", server.ID, files[0])
		if err := copyRegularFile(filepath.Join(sourceDir, files[0]), filepath.Join(stage, destinationRelative)); err != nil {
			return nil, err
		}
		server.SSH.KeyPath = filepath.Join(target, destinationRelative)
	}
	if migrations != nil {
		for i := range migrations.Transactions {
			txn := &migrations.Transactions[i]
			if findRoute(&inventory, txn.SourceRoute) == nil {
				return nil, errors.New("recovered migration references a missing source Route")
			}
			if replacement := findServer(&inventory, txn.ReplacementServer); replacement != nil {
				if txn.ReplacementServerContext != nil {
					txn.ReplacementServerContext["ssh_key_path"] = replacement.SSH.KeyPath
				}
			} else {
				sourceDir := filepath.Join(sourceRoot, "ssh", txn.ReplacementServer)
				entries, readErr := os.ReadDir(sourceDir)
				if readErr != nil {
					return nil, errors.New("recovery archive lacks active migration replacement SSH material")
				}
				files := []string{}
				for _, entry := range entries {
					if entry.Type().IsRegular() {
						files = append(files, entry.Name())
					}
				}
				if len(files) != 1 || txn.ReplacementServerContext == nil {
					return nil, errors.New("recovery archive has invalid active migration replacement SSH material")
				}
				destinationRelative := filepath.Join("ssh", txn.ReplacementServer, files[0])
				if err := copyRegularFile(filepath.Join(sourceDir, files[0]), filepath.Join(stage, destinationRelative)); err != nil {
					return nil, err
				}
				txn.ReplacementServerContext["ssh_key_path"] = filepath.Join(target, destinationRelative)
			}
			if findRoute(&inventory, txn.ReplacementRoute) != nil {
				if err := applyMigrationSelection(&inventory, txn, true); err != nil {
					return nil, err
				}
			}
			if findRoute(&inventory, txn.ReplacementRoute) != nil {
				txn.Phase = "replacement-prepared"
			} else {
				txn.Phase = "planned"
			}
			txn.PublicationAttempted = []string{}
			txn.LastFailure = "recovery-revalidation-required"
			txn.UpdatedAt = utcNow()
		}
		if err := writeJSONAtomic(filepath.Join(stage, migrationStateFile), migrations); err != nil {
			return nil, err
		}
	}
	inventory.Delivery.Directory = filepath.Join(target, "delivery")
	inventory.Delivery.RecoveryDirectory = filepath.Join(target, "recovery")
	inventory.Metadata.RecoveredAt = utcNow()
	if err := writeJSONAtomic(filepath.Join(stage, "inventory.json"), &inventory); err != nil {
		return nil, err
	}
	if err := writeJSONAtomic(filepath.Join(stage, "observed.json"), emptyObserved()); err != nil {
		return nil, err
	}
	if err := protectTree(stage); err != nil {
		return nil, err
	}
	if err := ValidateInventory(&inventory, stage, false); err != nil {
		return nil, err
	}
	if err := os.Rename(stage, target); err != nil {
		return nil, err
	}
	moved = true
	if err := protectTree(target); err != nil {
		return nil, err
	}
	finalState, err := LoadState(target)
	if err != nil {
		return nil, err
	}
	for _, server := range finalState.Inventory.Servers {
		if !regularFile(server.SSH.KeyPath) {
			return nil, errors.New("recovered SSH key is missing at its canonical path")
		}
	}
	completed = true
	return map[string]any{
		"restored":                        true,
		"inventory_schema":                finalState.Inventory.Schema,
		"servers":                         len(finalState.Inventory.Servers),
		"links":                           len(finalState.Inventory.Links),
		"routes":                          len(finalState.Inventory.Routes),
		"providers":                       len(finalState.Inventory.Providers),
		"manifest_files_verified":         verified,
		"observed_state_reset":            true,
		"migration_state_restored":        migrations != nil,
		"migration_revalidation_required": migrations != nil,
		"remote_changed":                  false,
	}, nil
}

func verifyRecoveryManifest(root string) (int, error) {
	manifest := filepath.Join(root, "SHA256SUMS")
	file, err := os.Open(manifest)
	if err != nil {
		return 0, errors.New("recovery archive is missing SHA256SUMS")
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	verified := 0
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, "  ", 2)
		if len(parts) != 2 || len(parts[0]) != 64 {
			return 0, errors.New("recovery manifest contains an invalid entry")
		}
		if _, err := hex.DecodeString(parts[0]); err != nil {
			return 0, errors.New("recovery manifest contains an invalid hash")
		}
		relative := filepath.FromSlash(parts[1])
		if filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return 0, errors.New("recovery manifest contains an unsafe path")
		}
		path, err := filepath.Abs(filepath.Join(root, relative))
		if err != nil {
			return 0, err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return 0, errors.New("recovery manifest path escapes archive")
		}
		actual, err := sha256File(path)
		if err != nil {
			return 0, errors.New("recovery archive is missing a declared file")
		}
		if !strings.EqualFold(actual, parts[0]) {
			return 0, errors.New("recovery archive failed SHA-256 manifest verification")
		}
		verified++
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	if verified < 2 {
		return 0, errors.New("recovery manifest is unexpectedly small")
	}
	return verified, nil
}

func writeRecoveryManifest(root string) error {
	files := []string{}
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() && filepath.Base(path) != "SHA256SUMS" {
			files = append(files, path)
		}
		return nil
	}); err != nil {
		return err
	}
	sort.Strings(files)
	var builder strings.Builder
	for _, path := range files {
		sum, err := sha256File(path)
		if err != nil {
			return err
		}
		relative, _ := filepath.Rel(root, path)
		fmt.Fprintf(&builder, "%s  %s\n", sum, filepath.ToSlash(relative))
	}
	return writeFileAtomic(filepath.Join(root, "SHA256SUMS"), []byte(builder.String()), 0o600)
}

func copyRegularFile(source, target string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("a recovery source file is not regular")
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	data, err := io.ReadAll(input)
	if err != nil {
		return err
	}
	return writeFileAtomic(target, data, 0o600)
}

func copyTree(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, _ := filepath.Rel(source, path)
		destination := filepath.Join(target, relative)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o700)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("refusing to copy a symbolic link")
		}
		return copyRegularFile(path, destination)
	})
}

func protectTree(root string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return protectPath(path, entry.IsDir())
	})
}

func resolveSevenZip(requested string) (string, error) {
	if requested != "" {
		path, err := filepath.Abs(requested)
		if err == nil && regularFile(path) {
			return path, nil
		}
		return "", errors.New("requested 7-Zip executable was not found")
	}
	for _, name := range []string{"7z", "7zz", "7z.exe"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	if os.Getenv("ProgramFiles") != "" {
		for _, path := range []string{
			filepath.Join(os.Getenv("ProgramFiles"), "7-Zip", "7z.exe"),
			filepath.Join(os.Getenv("ProgramFiles"), "NanaZip", "7z.exe"),
		} {
			if regularFile(path) {
				return path, nil
			}
		}
	}
	return "", errors.New("7-Zip (7z/7zz) was not found")
}

var runInteractive = func(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func timeStamp() string { return time.Now().UTC().Format("20060102T150405Z") }
