package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// SwaggerVariableReplacer processes Go files and replaces variable references in comments
type SwaggerVariableReplacer struct {
	constants map[string]interface{}
	patterns  []*regexp.Regexp
}

// NewSwaggerVariableReplacer creates a new replacer instance
func NewSwaggerVariableReplacer() *SwaggerVariableReplacer {
	return &SwaggerVariableReplacer{
		constants: make(map[string]interface{}),
		patterns: []*regexp.Regexp{
			// Pattern 1: {{VariableName}}
			regexp.MustCompile(`\{\{([A-Za-z_][A-Za-z0-9_]*)\}\}`),
			// Pattern 2: ${VariableName}
			regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`),
			// Pattern 3: @VAR(VariableName)
			regexp.MustCompile(`@VAR\(([A-Za-z_][A-Za-z0-9_]*)\)`),
		},
	}
}

// ProcessDirectory processes all Go files in a directory
func (r *SwaggerVariableReplacer) ProcessDirectory(dir string) error {
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			fmt.Printf("Processing: %s\n", path)
			return r.extractConstants(path)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to extract constants: %s", err.Error())
	}

	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			fmt.Printf("Processing: %s\n", path)
			return r.replaceVariablesInComments(path)
		}
		return nil
	})
}

// ProcessFile processes a single Go file
func (r *SwaggerVariableReplacer) ProcessFile(filename string) error {
	// Step 1: Parse the file to extract constants
	if err := r.extractConstants(filename); err != nil {
		return fmt.Errorf("failed to extract constants from %s: %v", filename, err)
	}

	// Step 2: Process comments and replace variables
	if err := r.replaceVariablesInComments(filename); err != nil {
		return fmt.Errorf("failed to replace variables in %s: %v", filename, err)
	}

	return nil
}

// extractConstants parses Go file and extracts constant declarations
func (r *SwaggerVariableReplacer) extractConstants(filename string) error {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filename, nil, parser.ParseComments)
	if err != nil {
		return err
	}

	ast.Inspect(node, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.GenDecl:
			if x.Tok == token.CONST {
				for _, spec := range x.Specs {
					if valueSpec, ok := spec.(*ast.ValueSpec); ok {
						for i, name := range valueSpec.Names {
							if i < len(valueSpec.Values) {
								value := r.extractValue(valueSpec.Values[i])
								if value != nil {
									r.constants[name.Name] = value
									// fmt.Printf("Found constant: %s = %v\n", name.Name, value)
								}
							}
						}
					}
				}
			}
		case *ast.ValueSpec:
			// Handle variable declarations
			if x.Type == nil { // inferred type
				for i, name := range x.Names {
					if i < len(x.Values) {
						value := r.extractValue(x.Values[i])
						if value != nil {
							r.constants[name.Name] = value
							// fmt.Printf("Found variable: %s = %v\n", name.Name, value)
						}
					}
				}
			}
		}
		return true
	})

	return nil
}

// extractValue extracts literal values from AST expressions
func (r *SwaggerVariableReplacer) extractValue(expr ast.Expr) interface{} {
	switch x := expr.(type) {
	case *ast.BasicLit:
		switch x.Kind {
		case token.INT:
			if val, err := strconv.Atoi(x.Value); err == nil {
				return val
			}
		case token.STRING:
			str := ""
			// Remove quotes
			if string(x.Value[0]) == `"` {
				str = strings.Trim(x.Value, `"`)
			} else {
				str = strings.Trim(x.Value, "`")
			}
			// Add `//` to each line for miltiline strings
			if strings.Contains(str, "\n") {
				lines := strings.Split(str, "\n")
				for i := 1; i < len(lines); i++ {
					lines[i] = "// " + lines[i]
				}
				str = strings.Join(lines, "\n")
			}
			return str
		case token.FLOAT:
			if val, err := strconv.ParseFloat(x.Value, 64); err == nil {
				return val
			}
		}
	case *ast.Ident:
		// Handle boolean literals
		switch x.Name {
		case "true":
			return true
		case "false":
			return false
		}
	}
	return nil
}

// replaceVariablesInComments reads file, replaces variables in comments, and writes back
func (r *SwaggerVariableReplacer) replaceVariablesInComments(filename string) error {
	// Read file
	content, err := os.ReadFile(filename)
	if err != nil {
		return err
	}

	lines := strings.Split(string(content), "\n")
	modified := false

	// Process each line
	for i, line := range lines {
		if strings.Contains(line, "//") {
			newLine := r.processCommentLine(line)
			if newLine != line {
				lines[i] = newLine
				modified = true
				fmt.Printf("Replaced: %s\n", line)
				fmt.Printf("    With: %s\n", newLine)
			}
		}
	}

	// Write back if modified
	if modified {
		newContent := strings.Join(lines, "\n")
		return os.WriteFile(filename, []byte(newContent), 0644)
	}

	return nil
}

// processCommentLine processes a single comment line and replaces variables
func (r *SwaggerVariableReplacer) processCommentLine(line string) string {
	result := line

	for _, pattern := range r.patterns {
		result = pattern.ReplaceAllStringFunc(result, func(match string) string {
			// Extract variable name
			submatches := pattern.FindStringSubmatch(match)
			if len(submatches) > 1 {
				varName := submatches[1]
				if value, exists := r.constants[varName]; exists {
					if strVal, isStr := value.(string); isStr {
						return fmt.Sprintf("%s", strVal)
					}
					return fmt.Sprintf("%v", value)
				}
				fmt.Printf("Warning: Variable '%s' not found\n", varName)
			}
			return match // Return original if not found
		})
	}

	return result
}

// Example usage with a sample Go file
func createSampleFile() {
	sampleCode := `package main

import (
	"github.com/gin-gonic/gin"
)

// HTTP Status Codes
const (
	StatusSuccess      = 200
	StatusCreated      = 201
	StatusBadRequest   = 400
	StatusUnauthorized = 401
	StatusNotFound     = 404
	StatusServerError  = 500
)

// Response Messages
const (
	MessageSuccess    = "Operation completed successfully"
	MessageCreated    = "Resource created successfully"
	MessageBadRequest = "Invalid request parameters"
	MessageNotFound   = "Resource not found"
)

// API Version
var APIVersion = "v1"

type User struct {
	ID   int    ` + "`" + `json:"id"` + "`" + `
	Name string ` + "`" + `json:"name"` + "`" + `
}

// Before processing (with variables):
// @Summary Get all users
// @Description Retrieve all users from the system
// @Tags users
// @Accept json
// @Produce json
// @Success {{StatusSuccess}} {object} User "{{MessageSuccess}}"
// @Failure {{StatusBadRequest}} {object} ErrorResponse "{{MessageBadRequest}}"
// @Failure {{StatusNotFound}} {object} ErrorResponse "{{MessageNotFound}}"
// @Failure {{StatusServerError}} {object} ErrorResponse "Server error"
// @Router /api/{{APIVersion}}/users [get]
func GetUsers(c *gin.Context) {
	// Implementation
	c.JSON(StatusSuccess, gin.H{"users": []User{}})
}

// Alternative syntax examples:
// @Success ${StatusCreated} {object} User "${MessageCreated}"
// @Success @VAR(StatusSuccess) {object} User "@VAR(MessageSuccess)"
func CreateUser(c *gin.Context) {
	c.JSON(StatusCreated, gin.H{"message": MessageCreated})
}
`

	err := ioutil.WriteFile("sample.go", []byte(sampleCode), 0644)
	if err != nil {
		log.Fatal("Failed to create sample file:", err)
	}
	fmt.Println("Created sample.go")
}

// Command-line interface
func main() {
	if len(os.Args) < 2 {
		fmt.Println("Swagger Variable Replacer")
		fmt.Println("Usage:")
		fmt.Println("  go run gofmtcomment <file.go>           - Process single file")
		fmt.Println("  go run gofmtcomment <directory>         - Process directory")
		fmt.Println("  go run gofmtcomment --sample           - Create sample file")
		fmt.Println("  go run gofmtcomment --help             - Show this help")
		fmt.Println("")
		fmt.Println("Supported variable patterns:")
		fmt.Println("  {{VariableName}}     - Double braces")
		fmt.Println("  ${VariableName}      - Dollar brace")
		fmt.Println("  @VAR(VariableName)   - Function-like")
		return
	}

	arg := os.Args[1]

	switch arg {
	case "--sample":
		createSampleFile()
		return
	case "--help":
		fmt.Println("This tool processes Go files and replaces variable references in comments.")
		fmt.Println("It extracts constants and variables from Go files and substitutes them in comments.")
		return
	}

	replacer := NewSwaggerVariableReplacer()

	// Check if argument is file or directory
	fileInfo, err := os.Stat(arg)
	if err != nil {
		log.Fatal("Error:", err)
	}

	if fileInfo.IsDir() {
		fmt.Printf("Processing directory: %s\n", arg)
		err = replacer.ProcessDirectory(arg)
	} else {
		fmt.Printf("Processing file: %s\n", arg)
		err = replacer.ProcessFile(arg)
	}

	if err != nil {
		log.Fatal("Error:", err)
	}

	fmt.Println("Processing completed!")
}

// Additional features you can add:

// 1. Configuration file support
type Config struct {
	Patterns     []string          `json:"patterns"`
	ExcludeFiles []string          `json:"exclude_files"`
	ConstantMap  map[string]string `json:"constant_map"`
}

// 2. Backup functionality
func (r *SwaggerVariableReplacer) BackupFile(filename string) error {
	content, err := ioutil.ReadFile(filename)
	if err != nil {
		return err
	}

	backupName := filename + ".backup"
	return ioutil.WriteFile(backupName, content, 0644)
}

// 3. Dry-run mode
func (r *SwaggerVariableReplacer) DryRun(filename string) error {
	// Process without writing back
	fmt.Printf("DRY RUN: Would modify %s\n", filename)
	return nil
}

// 4. Integration with go generate
// Add this comment to your Go files:
// //go:generate go run swagger-gofmtcomment .

// 5. Git hook integration
// Pre-commit hook that runs the replacer:
// #!/bin/sh
// go run main.go .
// git add -A

// 6. Watch mode (auto-process on file changes)
// Uses fsnotify package to watch for file changes
