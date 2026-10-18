package backup

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const restoreWorkDirName = ".tangentry-restore"

type RestoreJournal struct {
	PlanID         string                `json:"planId"`
	StagedID       string                `json:"stagedId"`
	RollbackID     string                `json:"rollbackId"`
	PreflightID    string                `json:"preflightId,omitempty"`
	Entries        []RestoreJournalEntry `json:"entries"`
	CurrentEntries []RestoreJournalEntry `json:"currentEntries"`
	Manifest       Manifest              `json:"manifest"`
	Step           string                `json:"step"`
	UpdatedAt      time.Time             `json:"updatedAt"`
	Error          string                `json:"error,omitempty"`
}

type RestoreJournalEntry struct {
	Name  string `json:"name"`
	State string `json:"state"`
}

const (
	entryPending           = "pending"
	entryMovedToRollback   = "moved-to-rollback"
	entryInstalled         = "installed"
	entryMovedToFailed     = "moved-to-failed"
	entryRestored          = "restored"
	MaxRestoreWorkDirSize  = 8 * MaxArchiveSize
	MaxPreRestoreSnapshots = 1
	restoreReservation     = 3 * MaxArchiveSize
)

var renameRestoreEntry = os.Rename

func restoreWorkDir(dataDir string) string { return filepath.Join(dataDir, restoreWorkDirName) }

func writeRestoreJournal(workDir string, journal RestoreJournal) error {
	journal.UpdatedAt = time.Now().UTC()
	data, err := json.Marshal(journal)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(workDir, ".journal-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := syncDirectory(workDir); err != nil {
		return err
	}
	if err := os.Rename(temporary, filepath.Join(workDir, "journal.json")); err != nil {
		return err
	}
	return syncDirectory(workDir)
}

func removeRestoreJournal(workDir string) error {
	if err := os.Remove(filepath.Join(workDir, "journal.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return syncDirectory(workDir)
}

func readRestoreJournal(workDir string) (RestoreJournal, error) {
	file, err := os.Open(filepath.Join(workDir, "journal.json"))
	if err != nil {
		return RestoreJournal{}, err
	}
	defer file.Close()
	var journal RestoreJournal
	if err := json.NewDecoder(io.LimitReader(file, 64<<10)).Decode(&journal); err != nil {
		return RestoreJournal{}, ErrInvalidPackage
	}
	if !validRestoreID(journal.StagedID) || !validRestoreID(journal.RollbackID) {
		return RestoreJournal{}, ErrInvalidPackage
	}
	if journal.PlanID != "" && !validRestoreID(journal.PlanID) {
		return RestoreJournal{}, ErrInvalidPackage
	}
	if journal.PreflightID != "" && !validRestoreID(journal.PreflightID) {
		return RestoreJournal{}, ErrInvalidPackage
	}
	if journal.PlanID != "" && (journal.PlanID != journal.StagedID || journal.PreflightID == "") {
		return RestoreJournal{}, ErrInvalidPackage
	}
	switch journal.Step {
	case "confirmed", "saving_current", "current_saved", "installing", "installed", "done", "rolling_back", "rolled_back":
	default:
		return RestoreJournal{}, ErrInvalidPackage
	}
	return journal, nil
}

func ApplyPendingRestore(dataDir string) error {
	workDir := restoreWorkDir(dataDir)
	if err := cleanupRestoreWorkDir(workDir); err != nil {
		return err
	}
	journal, err := readRestoreJournal(workDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := validateRestoreJournal(journal); err != nil {
		return err
	}
	staged := filepath.Join(workDir, "staged-"+journal.StagedID)
	rollback := filepath.Join(workDir, "rollback-"+journal.RollbackID)
	if journal.Step == "done" || journal.Step == "rolled_back" {
		if err := removeAllErr(staged); err != nil {
			return err
		}
		if err := syncDirectory(workDir); err != nil {
			return err
		}
		return removeRestoreJournal(workDir)
	}
	if journal.Step == "rolling_back" {
		return finishRestoreRollback(dataDir, workDir, staged, rollback, journal, errors.New("restore startup transaction rolled back"))
	}
	if journal.Step != "installed" && journal.Step != "installing" {
		if err := validateStaged(staged); err != nil {
			return err
		}
		if err := validateStagedManifest(staged, journal.Manifest); err != nil {
			return err
		}
	}
	if journal.Step == "confirmed" || journal.Step == "saving_current" {
		if err := os.MkdirAll(rollback, 0700); err != nil {
			return failRestoreAndRollback(dataDir, workDir, staged, rollback, journal, err)
		}
		if err := syncDirectory(workDir); err != nil {
			return failRestoreAndRollback(dataDir, workDir, staged, rollback, journal, err)
		}
		journal.Step = "saving_current"
		if err := writeRestoreJournal(workDir, journal); err != nil {
			return failRestoreAndRollback(dataDir, workDir, staged, rollback, journal, err)
		}
		if err := moveJournalEntries(dataDir, rollback, workDir, &journal, &journal.CurrentEntries, entryMovedToRollback); err != nil {
			return failRestoreAndRollback(dataDir, workDir, staged, rollback, journal, err)
		}
		journal.Step = "current_saved"
		if err := writeRestoreJournal(workDir, journal); err != nil {
			return failRestoreAndRollback(dataDir, workDir, staged, rollback, journal, err)
		}
	}
	if journal.Step == "current_saved" || journal.Step == "installing" {
		journal.Step = "installing"
		if err := writeRestoreJournal(workDir, journal); err != nil {
			return failRestoreAndRollback(dataDir, workDir, staged, rollback, journal, err)
		}
		if err := moveJournalEntries(staged, dataDir, workDir, &journal, &journal.Entries, entryInstalled); err != nil {
			return failRestoreAndRollback(dataDir, workDir, staged, rollback, journal, err)
		}
		journal.Step = "installed"
		if err := writeRestoreJournal(workDir, journal); err != nil {
			return failRestoreAndRollback(dataDir, workDir, staged, rollback, journal, err)
		}
	}
	if journal.Step == "installed" {
		journal.Step = "done"
		if err := writeRestoreJournal(workDir, journal); err != nil {
			return failRestoreAndRollback(dataDir, workDir, staged, rollback, journal, err)
		}
		if err := os.RemoveAll(staged); err != nil {
			return err
		}
		if err := syncDirectory(workDir); err != nil {
			return err
		}
		if err := pruneRollbackDirs(workDir, rollback); err != nil {
			return err
		}
		return removeRestoreJournal(workDir)
	}
	return nil
}

func cleanupRestoreWorkDir(workDir string) error {
	entries, err := os.ReadDir(workDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var journal *RestoreJournal
	if current, journalErr := readRestoreJournal(workDir); journalErr == nil {
		journal = &current
	} else if !errors.Is(journalErr, os.ErrNotExist) {
		return journalErr
	}
	keep := map[string]bool{}
	keepRollback := ""
	changed := false
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "plan-") || !strings.HasSuffix(name, ".json") {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(workDir, name))
		var plan restorePlan
		if readErr != nil || json.Unmarshal(data, &plan) != nil || !validRestoreID(plan.PlanID) || time.Since(plan.CreatedAt) > RestorePlanTTL {
			if err := os.Remove(filepath.Join(workDir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			changed = true
			continue
		}
		if validateRestorePlan(workDir, strings.TrimSuffix(strings.TrimPrefix(name, "plan-"), ".json"), plan) != nil {
			if err := os.Remove(filepath.Join(workDir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			changed = true
			continue
		}
		keep[name] = true
		keep["staged-"+plan.StagedID] = true
		keep["pre-"+plan.PreRestoreSnapshotID] = true
	}
	if journal != nil {
		keep["rollback-"+journal.RollbackID] = true
		if journal.Step != "done" && journal.Step != "rolled_back" {
			keep["staged-"+journal.StagedID] = true
		}
		if journal.Step == "rolling_back" {
			keep["failed-"+journal.StagedID] = true
		}
		if journal.PreflightID != "" {
			keep["pre-"+journal.PreflightID] = true
		}
		keepRollback = filepath.Join(workDir, "rollback-"+journal.RollbackID)
	}
	type preRestoreDir struct {
		name string
		mod  time.Time
	}
	var preRestoreDirs []preRestoreDir
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "pre-") || keep[entry.Name()] {
			continue
		}
		info, statErr := entry.Info()
		if statErr != nil {
			return statErr
		}
		if time.Since(info.ModTime()) > RestorePlanTTL {
			continue
		}
		preRestoreDirs = append(preRestoreDirs, preRestoreDir{name: entry.Name(), mod: info.ModTime()})
	}
	sort.Slice(preRestoreDirs, func(i, j int) bool { return preRestoreDirs[i].mod.After(preRestoreDirs[j].mod) })
	for i := 0; i < len(preRestoreDirs) && i < MaxPreRestoreSnapshots; i++ {
		keep[preRestoreDirs[i].name] = true
	}
	for _, entry := range entries {
		name := entry.Name()
		path := filepath.Join(workDir, name)
		if strings.HasPrefix(name, ".journal-") || strings.HasPrefix(name, "archive-") || strings.HasPrefix(name, "package-") || strings.HasPrefix(name, "restore-") || strings.HasPrefix(name, "validate-") {
			if err := os.RemoveAll(path); err != nil {
				return err
			}
			changed = true
			continue
		}
		if strings.HasPrefix(name, "pre-") && !keep[name] {
			if err := os.RemoveAll(path); err != nil {
				return err
			}
			changed = true
			continue
		}
		if (strings.HasPrefix(name, "failed-") || strings.HasPrefix(name, "staged-") || strings.HasPrefix(name, "plan-") && strings.HasSuffix(name, ".json")) && !keep[name] {
			if err := os.RemoveAll(path); err != nil {
				return err
			}
			changed = true
		}
	}
	if changed {
		if err := syncDirectory(workDir); err != nil {
			return err
		}
	}
	return pruneRollbackDirs(workDir, keepRollback)
}

func currentEntries(dataDir string, includeDownloads bool) ([]RestoreJournalEntry, error) {
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return nil, err
	}
	names := make([]RestoreJournalEntry, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if name == "tangentry.db" || name == "tangentry.db-wal" || name == "tangentry.db-shm" || name == "tangentry.db-journal" || name == "documents" || includeDownloads && name == "downloads" {
			names = append(names, RestoreJournalEntry{Name: name, State: entryPending})
		}
	}
	return names, nil
}

func pruneRollbackDirs(workDir, keep string) error {
	entries, err := os.ReadDir(workDir)
	if err != nil {
		return err
	}
	type rollbackDir struct {
		path string
		mod  time.Time
	}
	var dirs []rollbackDir
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "rollback-") && entry.IsDir() {
			info, infoErr := entry.Info()
			if infoErr != nil {
				return infoErr
			}
			dirs = append(dirs, rollbackDir{path: filepath.Join(workDir, entry.Name()), mod: info.ModTime()})
		}
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].mod.After(dirs[j].mod) })
	keepFound := false
	for _, dir := range dirs {
		if dir.path == keep {
			keepFound = true
			break
		}
	}
	keptLatest := false
	changed := false
	for _, dir := range dirs {
		if keepFound && dir.path == keep {
			continue
		}
		if !keepFound && !keptLatest {
			keptLatest = true
			continue
		}
		if err := os.RemoveAll(dir.path); err != nil {
			return err
		}
		changed = true
	}
	if changed {
		return syncDirectory(workDir)
	}
	return nil
}

func restoreWorkDirSize(workDir string) (int64, error) {
	var total int64
	entries, err := os.ReadDir(workDir)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	for _, entry := range entries {
		err := filepath.Walk(filepath.Join(workDir, entry.Name()), func(_ string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if !info.IsDir() {
				total += info.Size()
			}
			return nil
		})
		if err != nil {
			return 0, err
		}
		if total > MaxRestoreWorkDirSize {
			return total, ErrInvalidPackage
		}
	}
	return total, nil
}

func removeAllErr(path string) error {
	if path == "" {
		return nil
	}
	err := os.RemoveAll(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func failRestoreAndRollback(dataDir, workDir, staged, rollback string, journal RestoreJournal, cause error) error {
	failed := filepath.Join(workDir, "failed-"+journal.StagedID)
	if err := os.MkdirAll(failed, 0700); err != nil {
		return errors.Join(cause, err)
	}
	if err := syncDirectory(workDir); err != nil {
		return errors.Join(cause, err)
	}
	journal.Step = "rolling_back"
	journal.Error = "restore was rolled back during startup"
	if err := writeRestoreJournal(workDir, journal); err != nil {
		return errors.Join(cause, err)
	}
	return finishRestoreRollback(dataDir, workDir, staged, rollback, journal, cause)
}

func finishRestoreRollback(dataDir, workDir, staged, rollback string, journal RestoreJournal, cause error) error {
	failed := filepath.Join(workDir, "failed-"+journal.StagedID)
	for i := range journal.Entries {
		entry := &journal.Entries[i]
		if entry.State == entryPending {
			if _, err := os.Lstat(filepath.Join(staged, entry.Name)); errors.Is(err, os.ErrNotExist) {
				if _, dataErr := os.Lstat(filepath.Join(dataDir, entry.Name)); dataErr == nil {
					entry.State = entryInstalled
				} else if !errors.Is(dataErr, os.ErrNotExist) {
					return errors.Join(cause, dataErr)
				}
			} else if err != nil {
				return errors.Join(cause, err)
			}
		}
		if entry.State != entryInstalled {
			continue
		}
		if err := moveEntry(dataDir, failed, entry.Name); err != nil {
			return errors.Join(cause, err)
		}
		entry.State = entryMovedToFailed
		if err := writeRestoreJournal(workDir, journal); err != nil {
			return errors.Join(cause, err)
		}
	}
	for i := range journal.CurrentEntries {
		entry := &journal.CurrentEntries[i]
		if entry.State == entryPending {
			if _, err := os.Lstat(filepath.Join(rollback, entry.Name)); err == nil {
				entry.State = entryMovedToRollback
			} else if !errors.Is(err, os.ErrNotExist) {
				return errors.Join(cause, err)
			}
		}
		if entry.State != entryMovedToRollback {
			continue
		}
		if err := moveEntry(rollback, dataDir, entry.Name); err != nil {
			return errors.Join(cause, err)
		}
		entry.State = entryRestored
		if err := writeRestoreJournal(workDir, journal); err != nil {
			return errors.Join(cause, err)
		}
	}
	journal.Step = "rolled_back"
	if err := writeRestoreJournal(workDir, journal); err != nil {
		return errors.Join(cause, err)
	}
	if err := removeAllErr(staged); err != nil {
		return errors.Join(cause, err)
	}
	if err := syncDirectory(workDir); err != nil {
		return errors.Join(cause, err)
	}
	if err := removeRestoreJournal(workDir); err != nil {
		return errors.Join(cause, err)
	}
	return fmt.Errorf("restore startup transaction rolled back: %w", cause)
}

func moveJournalEntries(source, destination, workDir string, journal *RestoreJournal, entries *[]RestoreJournalEntry, state string) error {
	for i := range *entries {
		entry := &(*entries)[i]
		if entry.State != entryPending {
			continue
		}
		if err := moveEntry(source, destination, entry.Name); err != nil {
			return err
		}
		entry.State = state
		if err := writeRestoreJournal(workDir, *journal); err != nil {
			return err
		}
	}
	return nil
}

func moveEntry(source, destination, name string) error {
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name || name == restoreWorkDirName {
		return ErrInvalidPackage
	}
	from := filepath.Join(source, name)
	to := filepath.Join(destination, name)
	_, sourceErr := os.Lstat(from)
	_, destinationErr := os.Lstat(to)
	if errors.Is(sourceErr, os.ErrNotExist) {
		if destinationErr == nil {
			return nil
		}
		if errors.Is(destinationErr, os.ErrNotExist) {
			return fmt.Errorf("restore transaction entry missing")
		}
		return destinationErr
	}
	if sourceErr != nil {
		return sourceErr
	}
	if destinationErr == nil {
		return fmt.Errorf("restore transaction path conflict")
	}
	if !errors.Is(destinationErr, os.ErrNotExist) {
		return destinationErr
	}
	if err := renameRestoreEntry(from, to); err != nil {
		return err
	}
	if err := syncDirectory(source); err != nil {
		return err
	}
	if err := syncDirectory(destination); err != nil {
		return err
	}
	return nil
}

func journalEntries(names []string, state string) []RestoreJournalEntry {
	entries := make([]RestoreJournalEntry, 0, len(names))
	for _, name := range names {
		entries = append(entries, RestoreJournalEntry{Name: name, State: state})
	}
	return entries
}

func stagedEntries(staged string) ([]string, error) {
	entries, err := os.ReadDir(staged)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names, nil
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func validRestoreID(id string) bool {
	if len(id) != 32 {
		return false
	}
	for _, char := range id {
		if !strings.ContainsRune("0123456789abcdef", char) {
			return false
		}
	}
	return true
}

func validateJournalEntries(entries []RestoreJournalEntry, manifest Manifest, current bool) error {
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		name := entry.Name
		if name == "" || name == "." || name == ".." || filepath.Base(name) != name || name == restoreWorkDirName || strings.ContainsAny(name, `/\\`) || seen[name] {
			return ErrInvalidPackage
		}
		allowed := name == "tangentry.db" || name == "documents" || manifest.IncludeDownloads && name == "downloads"
		if current && (name == "tangentry.db-wal" || name == "tangentry.db-shm" || name == "tangentry.db-journal") {
			allowed = true
		}
		if !allowed {
			return ErrInvalidPackage
		}
		if current {
			if entry.State != entryPending && entry.State != entryMovedToRollback && entry.State != entryRestored {
				return ErrInvalidPackage
			}
		} else if entry.State != entryPending && entry.State != entryInstalled && entry.State != entryMovedToFailed {
			return ErrInvalidPackage
		}
		seen[name] = true
	}
	if !current {
		expected := map[string]bool{}
		for _, file := range manifest.Files {
			if file.Path == "tangentry.db" {
				expected["tangentry.db"] = true
				continue
			}
			root := strings.SplitN(file.Path, "/", 2)[0]
			expected[root] = true
		}
		if len(expected) != len(seen) {
			return ErrInvalidPackage
		}
		for name := range expected {
			if !seen[name] {
				return ErrInvalidPackage
			}
		}
	}
	return nil
}

func validateRestoreJournal(journal RestoreJournal) error {
	if err := validateJournalEntries(journal.Entries, journal.Manifest, false); err != nil {
		return err
	}
	if err := validateJournalEntries(journal.CurrentEntries, journal.Manifest, true); err != nil {
		return err
	}
	if len(journal.Entries) == 0 {
		return ErrInvalidPackage
	}
	return validateManifestFiles(journal.Manifest.Files, journal.Manifest.IncludeDownloads)
}
