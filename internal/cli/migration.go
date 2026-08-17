package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/josh/burrowtime/internal/store"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var watsonDataFiles = []string{"config", "frames", "state", "last_sync"}

const migrationOfferedFile = ".watson-migration-offered"

type migrationResult struct {
	Copied int
	Backup string
}

func (a *app) offerWatsonMigration(in io.Reader, out io.Writer, interactive bool) error {
	if !interactive || a.name == "watson" {
		return nil
	}
	destination, err := a.resolveDir()
	if err != nil {
		return err
	}
	source, err := store.WatsonDir()
	if err != nil {
		return err
	}
	same, err := samePath(source, destination)
	if err != nil || same {
		return err
	}
	if fileExists(filepath.Join(destination, migrationOfferedFile)) || directoryHasBurrowTimeData(destination) || !directoryHasWatsonData(source) {
		return nil
	}

	fmt.Fprintf(out, "BurrowTime is empty, but Watson data was found at %s.\n", source)
	yes, err := askYesNo(in, out, fmt.Sprintf("Import a copy into %s? [Y/n] ", destination), true)
	if err != nil {
		return err
	}
	if !yes {
		if err := markMigrationOffered(destination); err != nil {
			return err
		}
		fmt.Fprintln(out, "Starting fresh. You can import later with `burrowtime migrate from-watson`.")
		return nil
	}
	result, err := migrateCompatibleData(source, destination, true, "from-watson")
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Imported %d Watson data files. The original Watson data was left unchanged.\n\n", result.Copied)
	return nil
}

func (a *app) migrate() *cobra.Command {
	command := &cobra.Command{
		Use:   "migrate",
		Short: "Copy compatible data between BurrowTime and Watson.",
	}
	command.AddCommand(a.migrateFromWatson(), a.migrateToWatson())
	return command
}

func (a *app) migrateFromWatson() *cobra.Command {
	var watsonDir string
	var force bool
	command := &cobra.Command{
		Use:   "from-watson",
		Short: "Import a copy of an existing Watson data directory.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			destination, err := a.resolveDir()
			if err != nil {
				return err
			}
			source := watsonDir
			if source == "" {
				source, err = store.WatsonDir()
				if err != nil {
					return err
				}
			}
			if !directoryHasWatsonData(source) {
				return fmt.Errorf("No Watson data found at %s.", source)
			}
			if err := confirmMigrationOverwrite(cmd, destination, force, "BurrowTime"); err != nil {
				return err
			}
			result, err := migrateCompatibleData(source, destination, true, "from-watson")
			if err != nil {
				return err
			}
			printMigrationResult(cmd, result, "Imported", source, destination)
			return nil
		},
	}
	command.Flags().StringVar(&watsonDir, "watson-data-dir", "", "Watson data directory (defaults to WATSON_DIR)")
	command.Flags().BoolVarP(&force, "force", "f", false, "replace existing BurrowTime data without prompting")
	return command
}

func (a *app) migrateToWatson() *cobra.Command {
	var watsonDir string
	var force bool
	command := &cobra.Command{
		Use:   "to-watson",
		Short: "Export a copy of BurrowTime data for Watson.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			source, err := a.resolveDir()
			if err != nil {
				return err
			}
			if !directoryHasBurrowTimeData(source) {
				return fmt.Errorf("No BurrowTime data found at %s.", source)
			}
			repository, err := store.New(source)
			if err != nil {
				return err
			}
			concurrent, err := repository.LoadActiveTimers()
			if err != nil {
				return err
			}
			if len(concurrent) > 0 {
				return errors.New("Watson supports only one running timer; stop the additional timers before exporting")
			}
			destination := watsonDir
			if destination == "" {
				destination, err = store.WatsonDir()
				if err != nil {
					return err
				}
			}
			if err := confirmMigrationOverwrite(cmd, destination, force, "Watson"); err != nil {
				return err
			}
			result, err := migrateCompatibleData(source, destination, false, "to-watson")
			if err != nil {
				return err
			}
			printMigrationResult(cmd, result, "Exported", source, destination)
			return nil
		},
	}
	command.Flags().StringVar(&watsonDir, "watson-data-dir", "", "Watson data directory (defaults to WATSON_DIR)")
	command.Flags().BoolVarP(&force, "force", "f", false, "replace existing Watson data without prompting")
	return command
}

func confirmMigrationOverwrite(cmd *cobra.Command, destination string, force bool, label string) error {
	if !directoryHasMigrationTargetData(destination) || force {
		return nil
	}
	inFile, inOK := cmd.InOrStdin().(*os.File)
	outFile, outOK := cmd.OutOrStdout().(*os.File)
	if !inOK || !outOK || !term.IsTerminal(int(inFile.Fd())) || !term.IsTerminal(int(outFile.Fd())) {
		return fmt.Errorf("%s data already exists at %s; rerun with --force to replace it", label, destination)
	}
	yes, err := askYesNo(cmd.InOrStdin(), cmd.OutOrStdout(), fmt.Sprintf("Replace existing %s data at %s? A backup will be created. [y/N] ", label, destination), false)
	if err != nil {
		return err
	}
	if !yes {
		return errors.New("Aborted!")
	}
	return nil
}

func printMigrationResult(cmd *cobra.Command, result migrationResult, verb, source, destination string) {
	fmt.Fprintf(cmd.OutOrStdout(), "%s %d compatible data files from %s to %s.\n", verb, result.Copied, source, destination)
	if result.Backup != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "Previous destination data was backed up to %s.\n", result.Backup)
	}
	if verb == "Imported" {
		fmt.Fprintln(cmd.OutOrStdout(), "The original Watson data was left unchanged.")
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), "The original BurrowTime data was left unchanged.")
	}
}

func askYesNo(in io.Reader, out io.Writer, prompt string, defaultYes bool) (bool, error) {
	reader := bufio.NewReader(in)
	for {
		fmt.Fprint(out, prompt)
		answer, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return false, err
		}
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "":
			return defaultYes, nil
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			fmt.Fprintln(out, "Please answer yes or no.")
		}
		if errors.Is(err, io.EOF) {
			return defaultYes, nil
		}
	}
}

func directoryHasWatsonData(dir string) bool {
	return directoryHasFiles(dir, watsonDataFiles)
}

func directoryHasBurrowTimeData(dir string) bool {
	files := append(append([]string(nil), watsonDataFiles...), "active_timers")
	return directoryHasFiles(dir, files)
}

func directoryHasMigrationTargetData(dir string) bool {
	return directoryHasBurrowTimeData(dir)
}

func directoryHasFiles(dir string, names []string) bool {
	for _, name := range names {
		info, err := os.Stat(filepath.Join(dir, name))
		if err == nil && info.Mode().IsRegular() && info.Size() > 0 {
			return true
		}
	}
	return false
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func markMigrationOffered(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, migrationOfferedFile), []byte("Watson migration declined; run `burrowtime migrate from-watson` to import later.\n"), 0o600)
}

func samePath(first, second string) (bool, error) {
	first, err := filepath.Abs(first)
	if err != nil {
		return false, err
	}
	second, err = filepath.Abs(second)
	if err != nil {
		return false, err
	}
	return filepath.Clean(first) == filepath.Clean(second), nil
}

func migrateCompatibleData(source, destination string, clearBurrowExtras bool, direction string) (migrationResult, error) {
	same, err := samePath(source, destination)
	if err != nil {
		return migrationResult{}, err
	}
	if same {
		return migrationResult{}, errors.New("source and destination data directories are the same")
	}

	type fileData struct {
		data []byte
		mode os.FileMode
	}
	sourceFiles := make(map[string]fileData, len(watsonDataFiles))
	for _, name := range watsonDataFiles {
		path := filepath.Join(source, name)
		info, statErr := os.Stat(path)
		if errors.Is(statErr, os.ErrNotExist) {
			continue
		}
		if statErr != nil {
			return migrationResult{}, statErr
		}
		if !info.Mode().IsRegular() {
			return migrationResult{}, fmt.Errorf("migration source %s is not a regular file", path)
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return migrationResult{}, readErr
		}
		sourceFiles[name] = fileData{data: data, mode: info.Mode().Perm()}
	}
	if len(sourceFiles) == 0 {
		return migrationResult{}, fmt.Errorf("no compatible data files found at %s", source)
	}
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return migrationResult{}, err
	}

	targetNames := append([]string(nil), watsonDataFiles...)
	if clearBurrowExtras {
		targetNames = append(targetNames, "active_timers")
	}
	result := migrationResult{Copied: len(sourceFiles)}
	for _, name := range targetNames {
		path := filepath.Join(destination, name)
		info, statErr := os.Stat(path)
		if errors.Is(statErr, os.ErrNotExist) {
			continue
		}
		if statErr != nil {
			return migrationResult{}, statErr
		}
		if !info.Mode().IsRegular() {
			return migrationResult{}, fmt.Errorf("migration destination %s is not a regular file", path)
		}
		if result.Backup == "" {
			result.Backup, err = newMigrationBackupDir(destination, direction)
			if err != nil {
				return migrationResult{}, err
			}
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return migrationResult{}, readErr
		}
		if err := os.WriteFile(filepath.Join(result.Backup, name), data, info.Mode().Perm()); err != nil {
			return migrationResult{}, err
		}
	}

	for _, name := range targetNames {
		file, present := sourceFiles[name]
		path := filepath.Join(destination, name)
		if !present {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return migrationResult{}, err
			}
			continue
		}
		if err := atomicWriteMigrationFile(path, file.data, file.mode); err != nil {
			return migrationResult{}, err
		}
	}
	_ = os.Remove(filepath.Join(destination, migrationOfferedFile))
	return result, nil
}

func newMigrationBackupDir(destination, direction string) (string, error) {
	root := filepath.Join(destination, ".burrowtime-backups")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp(root, time.Now().UTC().Format("20060102T150405Z")+"-"+direction+"-")
	if err != nil {
		return "", err
	}
	return dir, nil
}

func atomicWriteMigrationFile(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	temporary, err := os.CreateTemp(dir, ".migration-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	ok := false
	defer func() {
		_ = temporary.Close()
		if !ok {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(mode); err != nil {
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	displaced := ""
	if _, err := os.Stat(path); err == nil {
		placeholder, createErr := os.CreateTemp(dir, ".migration-old-*")
		if createErr != nil {
			return createErr
		}
		displaced = placeholder.Name()
		if closeErr := placeholder.Close(); closeErr != nil {
			return closeErr
		}
		if removeErr := os.Remove(displaced); removeErr != nil {
			return removeErr
		}
		if renameErr := os.Rename(path, displaced); renameErr != nil {
			return renameErr
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		if displaced != "" {
			_ = os.Rename(displaced, path)
		}
		return err
	}
	if displaced != "" {
		_ = os.Remove(displaced)
	}
	ok = true
	return nil
}
