package backup

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const restoreWorkDirName = ".tangentry-restore"

type RestoreJournal struct {
	StagedID       string    `json:"stagedId"`
	RollbackID     string    `json:"rollbackId"`
	PreflightID    string    `json:"preflightId,omitempty"`
	Entries        []string  `json:"entries"`
	CurrentEntries []string  `json:"currentEntries"`
	Manifest       Manifest  `json:"manifest"`
	Step           string    `json:"step"`
	UpdatedAt      time.Time `json:"updatedAt"`
	Error          string    `json:"error,omitempty"`
}

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
	if err := os.Rename(temporary, filepath.Join(workDir, "journal.json")); err != nil {
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
	if journal.PreflightID != "" && !validRestoreID(journal.PreflightID) {
		return RestoreJournal{}, ErrInvalidPackage
	}
	return journal, nil
}

func ApplyPendingRestore(dataDir string) error {
	workDir := restoreWorkDir(dataDir)
	journal, err := readRestoreJournal(workDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if journal.Step == "done" || journal.Step == "rolled_back" {
		return nil
	}
	if err := validateJournalEntries(journal.Entries); err != nil {
		return err
	}
	if err := validateJournalEntries(journal.CurrentEntries); err != nil {
		return err
	}
	staged := filepath.Join(workDir, "staged-"+journal.StagedID)
	rollback := filepath.Join(workDir, "rollback-"+journal.RollbackID)
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
		journal.Step = "saving_current"
		if err := writeRestoreJournal(workDir, journal); err != nil {
			return failRestoreAndRollback(dataDir, workDir, staged, rollback, journal, err)
		}
		if err := moveNamedEntries(dataDir, rollback, journal.CurrentEntries); err != nil {
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
		if err := moveNamedEntries(staged, dataDir, journal.Entries); err != nil {
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
		return syncDirectory(workDir)
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
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".journal-") {
			_ = os.Remove(filepath.Join(workDir, entry.Name()))
			continue
		}
		if strings.HasPrefix(entry.Name(), "archive-") || strings.HasPrefix(entry.Name(), "package-") || strings.HasPrefix(entry.Name(), "restore-") || strings.HasPrefix(entry.Name(), "validate-") {
			_ = os.RemoveAll(filepath.Join(workDir, entry.Name()))
			continue
		}
		if strings.HasPrefix(entry.Name(), "pre-") {
			info, statErr := entry.Info()
			if statErr == nil && time.Since(info.ModTime()) > RestorePlanTTL {
				_ = os.RemoveAll(filepath.Join(workDir, entry.Name()))
			}
		}
	}
	return nil
}

func currentEntries(dataDir string) ([]string, error) {
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Name() == restoreWorkDirName || entry.Name() == "admin-password" {
			continue
		}
		names = append(names, entry.Name())
	}
	return names, nil
}

func restoreNamedEntries(source, destination string, names []string) error {
	for _, name := range names {
		if _, err := os.Lstat(filepath.Join(source, name)); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		if _, err := os.Lstat(filepath.Join(destination, name)); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.Rename(filepath.Join(source, name), filepath.Join(destination, name)); err != nil {
			return err
		}
		if err := syncDirectory(source); err != nil {
			return err
		}
		if err := syncDirectory(destination); err != nil {
			return err
		}
	}
	return nil
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
	if journal.Step == "installing" || journal.Step == "installed" {
		if err := moveNamedEntries(dataDir, failed, journal.Entries); err != nil {
			return errors.Join(cause, err)
		}
	}
	if err := restoreNamedEntries(rollback, dataDir, journal.CurrentEntries); err != nil {
		return errors.Join(cause, err)
	}
	journal.Step = "rolled_back"
	journal.Error = "restore was rolled back during startup"
	if err := writeRestoreJournal(workDir, journal); err != nil {
		return errors.Join(cause, err)
	}
	_ = os.RemoveAll(staged)
	return fmt.Errorf("restore startup transaction rolled back: %w", cause)
}

func moveNamedEntries(source, destination string, names []string) error {
	for _, name := range names {
		if name == "" || name == "." || name == ".." || filepath.Base(name) != name || name == restoreWorkDirName {
			return ErrInvalidPackage
		}
		from := filepath.Join(source, name)
		to := filepath.Join(destination, name)
		_, sourceErr := os.Lstat(from)
		_, destinationErr := os.Lstat(to)
		if errors.Is(sourceErr, os.ErrNotExist) {
			// A crash may have happened after the rename but before the
			// journal step was persisted. Treat the already-moved entry as
			// complete so startup can safely retry the phase.
			if errors.Is(destinationErr, os.ErrNotExist) {
				return fmt.Errorf("restore transaction entry missing")
			}
			if destinationErr != nil {
				return destinationErr
			}
			continue
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
		if err := os.Rename(from, to); err != nil {
			return err
		}
		if err := syncDirectory(source); err != nil {
			return err
		}
		if err := syncDirectory(destination); err != nil {
			return err
		}
	}
	return nil
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

func validateJournalEntries(entries []string) error {
	seen := make(map[string]bool, len(entries))
	for _, name := range entries {
		if name == "" || name == "." || name == ".." || filepath.Base(name) != name || name == restoreWorkDirName || strings.ContainsAny(name, `/\\`) || seen[name] {
			return ErrInvalidPackage
		}
		seen[name] = true
	}
	return nil
}
