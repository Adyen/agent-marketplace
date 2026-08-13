package skill

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type skillFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Metadata    struct {
		Tags []string `yaml:"tags"`
	} `yaml:"metadata"`
}

func parseFrontmatter(skillMdPath string) (name, description string, tags []string, err error) {
	file, err := os.Open(skillMdPath)
	if err != nil {
		return "", "", nil, fmt.Errorf("read file: %w", err)
	}
	defer func() {
		err = errors.Join(err, file.Close())
	}()

	frontmatter, err := readFrontmatter(file)
	if err != nil {
		return "", "", nil, err
	}

	var parsed skillFrontmatter
	if err := yaml.Unmarshal(frontmatter, &parsed); err != nil {
		return "", "", nil, fmt.Errorf("parse yaml: %w", err)
	}

	return parsed.Name, parsed.Description, parsed.Metadata.Tags, nil
}

func readFrontmatter(file *os.File) ([]byte, error) {
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1024), 64*1024)

	var builder strings.Builder
	foundStart := false
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(strings.TrimLeft(line, "\ufeff"))
		if !foundStart {
			if trimmed != "---" {
				continue
			}
			foundStart = true
			continue
		}

		if trimmed == "---" {
			return []byte(builder.String()), nil
		}

		if builder.Len() > 0 {
			builder.WriteByte('\n')
		}
		builder.WriteString(line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	if !foundStart {
		return nil, fmt.Errorf("missing opening frontmatter delimiter")
	}
	return nil, fmt.Errorf("missing closing frontmatter delimiter")
}
