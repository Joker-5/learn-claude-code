// s05_skill_loading.go - Two-layer skill injection to avoid bloating system prompt.
package agents

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Skill represents a loaded skill with metadata and body.
type Skill struct {
	Meta map[string]string // YAML frontmatter key-value pairs
	Body string            // Skill content after frontmatter
	Path string            // File path where skill was found
}

// SkillLoader scans skills/ directories for SKILL.md files.
type SkillLoader struct {
	SkillsDir string
	Skills    map[string]Skill
}

var frontmatterRegex = regexp.MustCompile(`(?s)^---\n(.*?)\n---\n(.*)`)

func NewSkillLoader(skillsDir string) *SkillLoader {
	sl := &SkillLoader{
		SkillsDir: skillsDir,
		Skills:    make(map[string]Skill),
	}
	sl.loadAll()
	return sl
}

func (sl *SkillLoader) loadAll() {
	dir, err := os.Open(sl.SkillsDir)
	if err != nil {
		return
	}
	defer dir.Close()

	filepath.Walk(sl.SkillsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if info.Name() == "SKILL.md" {
			sl.loadSkillFile(path)
		}
		return nil
	})
}

func (sl *SkillLoader) loadSkillFile(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	text := string(data)
	meta := make(map[string]string)
	body := text

	match := frontmatterRegex.FindStringSubmatch(text)
	if match != nil {
		// Parse simple YAML-like frontmatter
		lines := strings.Split(match[1], "\n")
		for _, line := range lines {
			if idx := strings.Index(line, ":"); idx > 0 {
				key := strings.TrimSpace(line[:idx])
				val := strings.TrimSpace(line[idx+1:])
				meta[key] = val
			}
		}
		body = strings.TrimSpace(match[2])
	}

	name := meta["name"]
	if name == "" {
		name = filepath.Base(filepath.Dir(path))
	}
	sl.Skills[name] = Skill{
		Meta: meta,
		Body: body,
		Path: path,
	}
}

// GetDescriptions returns Layer 1: skill names and descriptions for system prompt.
func (sl *SkillLoader) GetDescriptions() string {
	if len(sl.Skills) == 0 {
		return "(no skills available)"
	}
	var lines []string
	for name, skill := range sl.Skills {
		desc := skill.Meta["description"]
		if desc == "" {
			desc = "No description"
		}
		tags := skill.Meta["tags"]
		line := "  - " + name + ": " + desc
		if tags != "" {
			line += " [" + tags + "]"
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// GetContent returns Layer 2: full skill body wrapped in <skill> tags.
func (sl *SkillLoader) GetContent(name string) string {
	skill, ok := sl.Skills[name]
	if !ok {
		var keys []string
		for k := range sl.Skills {
			keys = append(keys, k)
		}
		return "Error: Unknown skill '" + name + "'. Available: " + strings.Join(keys, ", ")
	}
	return "<skill name=\"" + name + "\">\n" + skill.Body + "\n</skill>"
}

// SkillToolDefinition returns the tool schema for load_skill.
func SkillToolDefinition() ToolDefinition {
	return ToolDefinition{
		Name:        "load_skill",
		Description: "Load specialized knowledge by name.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Skill name to load",
				},
			},
			"required": []string{"name"},
		},
	}
}
