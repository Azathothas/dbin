package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/goccy/go-json"
	"github.com/pkg/xattr"
	"github.com/zeebo/blake3"
)

// removeDuplicates removes duplicate binaries from the list (used in ./install.go)
func removeDuplicates(binaries []string) []string {
	seen := make(map[string]struct{})
	result := []string{}
	for _, binary := range binaries {
		if _, ok := seen[binary]; !ok {
			seen[binary] = struct{}{}
			result = append(result, binary)
		}
	}
	return result
}

// contanins will return true if the provided slice of []strings contains the word str
func contains(slice []string, str string) bool {
	for _, v := range slice {
		if v == str {
			return true
		}
	}
	return false
}

// fileExists checks if a file exists.
func fileExists(filePath string) bool {
	_, err := os.Stat(filePath)
	return !os.IsNotExist(err)
}

// isDirectory checks if the given path is a directory.
func isDirectory(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil // Path does not exist
		}
		return false, err
	}
	return info.IsDir(), nil
}

// isExecutable checks if the file at the specified path is executable.
func isExecutable(filePath string) bool {
	info, err := os.Stat(filePath)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular() && (info.Mode().Perm()&0o111) != 0
}

// validateProgramsFrom checks the validity of programs against a remote source
func validateProgramsFrom(config *Config, programsToValidate []string) ([]string, error) {
	installDir := config.InstallDir
	remotePrograms, err := listBinaries(config)
	if err != nil {
		return nil, fmt.Errorf("failed to list remote binaries: %w", err)
	}

	files, err := listFilesInDir(installDir)
	if err != nil {
		return nil, fmt.Errorf("failed to list files in %s: %w", installDir, err)
	}

	programsToValidate = removeDuplicates(programsToValidate)
	validPrograms := make([]string, 0, len(programsToValidate))

	// Inline function to validate a file against the remote program list
	validate := func(file string) (string, bool) {
		fullBinaryName := listInstalled(file) // Get the full binary name of the file
		if config.RetakeOwnership == true {
			fullBinaryName = filepath.Base(file)
			if fullBinaryName == "" {
				return "", false // If we couldn't get a valid name, return invalid
			}
		}
		// Check if the full name exists in the remote programs
		if contains(remotePrograms, fullBinaryName) {
			return fullBinaryName, true
		}
		return "", false
	}

	if len(programsToValidate) == 0 {
		// Validate all files in the directory
		for _, file := range files {
			if fullName, valid := validate(file); valid {
				validPrograms = append(validPrograms, fullName)
			}
		}
	} else {
		// Validate only the specified programs
		for _, program := range programsToValidate {
			file := filepath.Join(installDir, program)
			if fullName, valid := validate(file); valid {
				validPrograms = append(validPrograms, fullName)
			}
		}
	}

	return validPrograms, nil
}

func listInstalled(binaryPath string) string {
	if isSymlink(binaryPath) {
		return ""
	}
	// Retrieve the fullName of the binary
	fullBinaryName, err := getFullName(binaryPath)
	if err != nil || fullBinaryName == "" {
		return "" // If we can't get the full name, consider it invalid
	}
	return fullBinaryName
}

// errorEncoder generates a unique error code based on the sum of ASCII values of the error message.
func errorEncoder(format string, args ...interface{}) int {
	formattedErrorMessage := fmt.Sprintf(format, args...)

	var sum int
	for _, char := range formattedErrorMessage {
		sum += int(char)
	}
	errorCode := sum % 256
	fmt.Fprint(os.Stderr, formattedErrorMessage)
	return errorCode
}

// errorOut prints the error message to stderr and exits the program with the error code generated by errorEncoder.
func errorOut(format string, args ...interface{}) {
	os.Exit(errorEncoder(format, args...))
}

// GetTerminalWidth attempts to determine the width of the terminal.
// It first tries using "stty size", then "tput cols", and finally falls back to  80 columns.
func getTerminalWidth() int {
	// Try using stty size
	cmd := exec.Command("stty", "size")
	cmd.Stdin = os.Stdin
	out, err := cmd.Output()
	if err == nil {
		// stty size returns rows and columns
		parts := strings.Split(strings.TrimSpace(string(out)), " ")
		if len(parts) == 2 {
			width, _ := strconv.Atoi(parts[1])
			return width
		}
	}

	// Fallback to tput cols
	cmd = exec.Command("tput", "cols")
	cmd.Stdin = os.Stdin
	out, err = cmd.Output()
	if err == nil {
		width, _ := strconv.Atoi(strings.TrimSpace(string(out)))
		return width
	}

	// Fallback to  80 columns
	return 80
}

// NOTE: \n will always get cut off when using a truncate function, this may also happen to other formatting options
// truncateSprintf formats the string and truncates it if it exceeds the terminal width.
func truncateSprintf(indicator, format string, a ...interface{}) string {
	// Format the string first
	formatted := fmt.Sprintf(format, a...)

	// Check if output is piped
	if isPipedOutput() {
		return formatted // No truncation if output is being piped to another program
	}

	// Determine the truncation length & truncate the formatted string if it exceeds the available space
	availableSpace := getTerminalWidth() - len(indicator)
	if len(formatted) > availableSpace {
		formatted = formatted[:availableSpace]
		// Remove trailing punctuation and spaces
		for strings.HasSuffix(formatted, ",") || strings.HasSuffix(formatted, ".") || strings.HasSuffix(formatted, " ") {
			formatted = formatted[:len(formatted)-1]
		}
		formatted = fmt.Sprintf("%s%s", formatted, indicator) // Add the indicator (the dots)
	}

	return formatted
}

// truncatePrintf is a drop-in replacement for fmt.Printf that truncates the input string if it exceeds a certain length.
func truncatePrintf(disableTruncation, addNewLine bool, format string, a ...interface{}) (n int, err error) {
	if disableTruncation {
		return fmt.Printf(format, a...)
	}
	if addNewLine {
		return fmt.Println(truncateSprintf(indicator, format, a...))
	}
	return fmt.Print(truncateSprintf(indicator, format, a...))
}

// listFilesInDir lists all files in a directory
func listFilesInDir(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			files = append(files, filepath.Join(dir, entry.Name()))
		}
	}
	return files, nil
}

// getFullName retrieves the full binary name from the extended attributes of the binary file.
// If the binary does not exist, it returns the basename. If the full name attribute cannot be retrieved, it returns an error.
func getFullName(binaryPath string) (string, error) {
	// Check if the binary file exists using the existing isFileExist function
	if !fileExists(binaryPath) {
		// Return the basename if the file doesn't exist
		return filepath.Base(binaryPath), nil
	}

	// Retrieve the "user.FullName" attribute
	fullName, err := xattr.Get(binaryPath, "user.FullName")
	if err != nil {
		// Return an error if the full name cannot be retrieved but the binary exists
		return "", fmt.Errorf("full name attribute not found for binary: %s", binaryPath)
	}

	return string(fullName), nil
}

// addFullName writes the full binary name to the extended attributes of the binary file.
func addFullName(binaryPath string, fullName string) error {
	// Set the "user.FullName" attribute
	if err := xattr.Set(binaryPath, "user.FullName", []byte(fullName)); err != nil {
		return fmt.Errorf("failed to set xattr for %s: %w", binaryPath, err)
	}
	return nil
}

// removeNixGarbageFoundInTheRepos corrects any /nix/store/ or /bin/ binary path in the file.
func removeNixGarbageFoundInTheRepos(filePath string) error {
	// Read the entire file content
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file %s: %v", filePath, err)
	}
	// Regex to match and remove the /nix/store/.../ prefix in the shebang line, preserving the rest of the path
	nixShebangRegex := regexp.MustCompile(`^#!\s*/nix/store/[^/]+/`)
	// Regex to match and remove the /nix/store/*/bin/ prefix in other lines
	nixBinPathRegex := regexp.MustCompile(`/nix/store/[^/]+/bin/`)
	// Split content by lines
	lines := strings.Split(string(content), "\n")
	// Flag to track if any corrections were made
	correctionsMade := false
	// Handle the shebang line separately if it exists and matches the nix pattern
	if len(lines) > 0 && nixShebangRegex.MatchString(lines[0]) {
		lines[0] = nixShebangRegex.ReplaceAllString(lines[0], "#!/")
		// Iterate through the rest of the lines and correct any /nix/store/*/bin/ path
		for i := 1; i < len(lines); i++ {
			if nixBinPathRegex.MatchString(lines[i]) {
				lines[i] = nixBinPathRegex.ReplaceAllString(lines[i], "")
			}
		}
		correctionsMade = true
	}
	// If any corrections were made, write the modified content back to the file
	if correctionsMade {
		if err := os.WriteFile(filePath, []byte(strings.Join(lines, "\n")), 0644); err != nil {
			return fmt.Errorf("failed to correct nix object [%s]: %v", filepath.Base(filePath), err)
		}
		fmt.Printf("[%s] is a nix object. Corrections have been made.\n", filepath.Base(filePath))
	}
	return nil
}

func fetchJSON(url string, v interface{}) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("error creating request for %s: %v", url, err)
	}

	req.Header.Set("Cache-Control", "no-cache, no-store, must-revalidate")
	req.Header.Set("Pragma", "no-cache")
	req.Header.Set("Expires", "0")

	client := &http.Client{}
	response, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("error fetching from %s: %v", url, err)
	}
	defer response.Body.Close()

	body := &bytes.Buffer{}
	if _, err := io.Copy(body, response.Body); err != nil {
		return fmt.Errorf("error reading from %s: %v", url, err)
	}

	if err := json.Unmarshal(body.Bytes(), v); err != nil {
		return fmt.Errorf("error decoding from %s: %v", url, err)
	}

	return nil
}

// calculateChecksum calculates the checksum of a file
func calculateChecksum(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher := blake3.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", hasher.Sum(nil)), nil
}

// isPipedOutput checks if the output is piped
func isPipedOutput() bool {
	// Check if stdout is a pipe
	fileInfo, err := os.Stdout.Stat()
	if err != nil {
		return false // Default to not piped if there's an error
	}
	return (fileInfo.Mode() & os.ModeNamedPipe) != 0
}

func isSymlink(filePath string) bool {
	fileInfo, err := os.Lstat(filePath)
	return err == nil && fileInfo.Mode()&os.ModeSymlink != 0
}
