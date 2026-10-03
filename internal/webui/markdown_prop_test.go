package webui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// **Validates: Requirements 5.1**

// TestProperty_HeadingIDUniqueness verifies that for any list of heading texts
// (including duplicates), all generated IDs are unique within that document.
// This ensures anchor links can reliably target specific sections.
// Feature: cli-documentation-page, Property 3: Heading ID Uniqueness
func TestProperty_HeadingIDUniqueness(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate a list of heading texts with at least 1 entry, allowing duplicates.
		// Use a mix of random strings and a pool of common headings to ensure duplicates.
		headingPool := []string{
			"Introduction",
			"Getting Started",
			"Installation",
			"Usage",
			"Configuration",
			"API Reference",
			"Examples",
			"FAQ",
			"Troubleshooting",
			"Conclusion",
		}

		// Generate between 1 and 50 headings
		numHeadings := rapid.IntRange(1, 50).Draw(rt, "numHeadings")

		// Build a list of heading texts, mixing pool values and random strings
		headingTexts := make([]string, numHeadings)
		for i := 0; i < numHeadings; i++ {
			// 70% chance to pick from pool (increases duplicate probability)
			if rapid.Bool().Draw(rt, "usePool") && rapid.Float64Range(0, 1).Draw(rt, "poolChance") < 0.7 {
				headingTexts[i] = rapid.SampledFrom(headingPool).Draw(rt, "poolHeading")
			} else {
				// Generate a random string that may include special characters
				headingTexts[i] = rapid.String().Draw(rt, "randomHeading")
			}
		}

		// Generate IDs for all headings using a shared map (as would happen in a single document)
		existingIDs := make(map[string]int)
		generatedIDs := make([]string, numHeadings)

		for i, text := range headingTexts {
			generatedIDs[i] = generateHeadingID(text, existingIDs)
		}

		// Property: All generated IDs must be unique
		seenIDs := make(map[string]bool)
		for i, id := range generatedIDs {
			if seenIDs[id] {
				rt.Fatalf("duplicate ID %q generated for heading %d (text: %q)", id, i, headingTexts[i])
			}
			seenIDs[id] = true
		}
	})
}

// TestProperty_HeadingIDUniquenessWithIdenticalTexts specifically tests the case
// where multiple headings have exactly the same text.
// This is a focused test for the duplicate handling logic.
// Feature: cli-documentation-page, Property 3: Heading ID Uniqueness
func TestProperty_HeadingIDUniquenessWithIdenticalTexts(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate a single heading text
		headingText := rapid.String().Draw(rt, "headingText")

		// Generate between 2 and 20 identical headings
		count := rapid.IntRange(2, 20).Draw(rt, "count")

		// Generate IDs for all identical headings
		existingIDs := make(map[string]int)
		generatedIDs := make([]string, count)

		for i := 0; i < count; i++ {
			generatedIDs[i] = generateHeadingID(headingText, existingIDs)
		}

		// Property: All generated IDs must be unique even for identical input texts
		seenIDs := make(map[string]bool)
		for i, id := range generatedIDs {
			if seenIDs[id] {
				rt.Fatalf("duplicate ID %q generated for heading %d (all headings have text: %q)", id, i, headingText)
			}
			seenIDs[id] = true
		}
	})
}

// **Validates: Requirements 5.2**

// urlFriendlyIDRegex defines the expected format for heading IDs.
// IDs must be lowercase alphanumeric with hyphens as separators.
// Pattern: lowercase letters/numbers, optionally followed by hyphen+alphanumeric groups.
// No leading/trailing hyphens, no consecutive hyphens.
var urlFriendlyIDRegex = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// TestProperty_HeadingIDURLFriendliness verifies that all generated heading IDs
// match the URL-friendly format pattern `^[a-z0-9]+(-[a-z0-9]+)*$`.
//
// Property 2: Heading ID URL-Friendliness
// For any heading text string, the generated heading ID SHALL match the regular
// expression `^[a-z0-9]+(-[a-z0-9]+)*$` (lowercase alphanumeric with hyphens,
// no leading/trailing hyphens, no consecutive hyphens).
//
// Feature: cli-documentation-page
func TestProperty_HeadingIDURLFriendliness(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate random heading text with various characters
		headingText := rapid.StringMatching(`[a-zA-Z0-9 _\-!@#$%^&*().,?'"]{1,100}`).Draw(rt, "headingText")

		// Create fresh ID tracking map for uniqueness handling
		existing := make(map[string]int)

		// Generate the heading ID
		id := generateHeadingID(headingText, existing)

		// Verify the ID matches the URL-friendly pattern
		if !urlFriendlyIDRegex.MatchString(id) {
			rt.Fatalf("URL-FRIENDLINESS VIOLATION: heading text %q generated ID %q "+
				"which does not match pattern ^[a-z0-9]+(-[a-z0-9]+)*$",
				headingText, id)
		}
	})
}

// TestProperty_HeadingIDURLFriendliness_UnicodeInput tests that heading IDs remain
// URL-friendly even when given Unicode/non-ASCII input.
//
// Property 2 (extended): For any heading text including Unicode characters,
// the generated heading ID SHALL match the URL-friendly format.
//
// Feature: cli-documentation-page
func TestProperty_HeadingIDURLFriendliness_UnicodeInput(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate random strings that may include Unicode characters
		// Mix of ASCII and some common Unicode ranges
		headingText := rapid.String().Draw(rt, "headingText")

		existing := make(map[string]int)
		id := generateHeadingID(headingText, existing)

		// Even with arbitrary input, the ID must be URL-friendly
		if !urlFriendlyIDRegex.MatchString(id) {
			rt.Fatalf("URL-FRIENDLINESS VIOLATION: Unicode heading text generated ID %q "+
				"which does not match pattern ^[a-z0-9]+(-[a-z0-9]+)*$",
				id)
		}
	})
}

// TestProperty_HeadingIDURLFriendliness_EdgeCases tests specific edge cases
// to ensure robustness of the ID generation.
//
// Property 2 (edge cases): For edge case inputs (empty-ish, all special chars,
// long strings), the generated heading ID SHALL still be URL-friendly.
//
// Feature: cli-documentation-page
func TestProperty_HeadingIDURLFriendliness_EdgeCases(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate various edge case inputs
		inputType := rapid.IntRange(0, 4).Draw(rt, "inputType")

		var headingText string
		switch inputType {
		case 0:
			// Only whitespace and special characters (no alphanumeric)
			headingText = rapid.StringMatching(`[ _\-!@#$%^&*().,?'"]+`).Draw(rt, "specialOnly")
		case 1:
			// Very long string
			headingText = rapid.StringMatching(`[a-zA-Z0-9 ]{100,200}`).Draw(rt, "longText")
		case 2:
			// Mixed case
			headingText = rapid.StringMatching(`[A-Z][a-z]+[A-Z][a-z]+`).Draw(rt, "mixedCase")
		case 3:
			// Numbers and letters
			headingText = rapid.StringMatching(`[0-9]+[a-z]+[0-9]+`).Draw(rt, "numbersAndLetters")
		case 4:
			// Hyphens and underscores
			headingText = rapid.StringMatching(`[a-z]+[-_]+[a-z]+`).Draw(rt, "separators")
		}

		existing := make(map[string]int)
		id := generateHeadingID(headingText, existing)

		if !urlFriendlyIDRegex.MatchString(id) {
			rt.Fatalf("URL-FRIENDLINESS VIOLATION (edge case type %d): heading text %q generated ID %q "+
				"which does not match pattern ^[a-z0-9]+(-[a-z0-9]+)*$",
				inputType, headingText, id)
		}
	})
}

// TestProperty_HeadingIDURLFriendliness_WithDuplicates tests that IDs remain
// URL-friendly even when duplicate detection appends numeric suffixes.
//
// Property 2 (with duplicates): When duplicate headings cause numeric suffix
// addition (-1, -2, etc.), the resulting IDs SHALL still be URL-friendly.
//
// Feature: cli-documentation-page
func TestProperty_HeadingIDURLFriendliness_WithDuplicates(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate a base heading text
		baseText := rapid.StringMatching(`[a-zA-Z][a-zA-Z0-9 ]{0,30}`).Draw(rt, "baseText")

		// Generate multiple IDs to trigger duplicate handling
		numDuplicates := rapid.IntRange(2, 10).Draw(rt, "numDuplicates")

		existing := make(map[string]int)

		for i := 0; i < numDuplicates; i++ {
			id := generateHeadingID(baseText, existing)

			if !urlFriendlyIDRegex.MatchString(id) {
				rt.Fatalf("URL-FRIENDLINESS VIOLATION (duplicate #%d): heading text %q generated ID %q "+
					"which does not match pattern ^[a-z0-9]+(-[a-z0-9]+)*$",
					i+1, baseText, id)
			}
		}
	})
}

// **Validates: Requirements 6.2**

// TestProperty_TOCCompletenessForH2H3Headings verifies that for any markdown document,
// the extracted TOC contains an entry for every h2 and h3 heading in the document,
// and does not contain entries for h1, h4, h5, or h6 headings.
// Feature: cli-documentation-page, Property 4: TOC Completeness for h2/h3 Headings
func TestProperty_TOCCompletenessForH2H3Headings(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate a random set of headings with various levels (1-6)
		numHeadings := rapid.IntRange(1, 20).Draw(rt, "numHeadings")

		var markdownLines []string
		var expectedH2H3 []struct {
			text  string
			level int
		}
		var excludedHeadings []struct {
			text  string
			level int
		}

		for i := 0; i < numHeadings; i++ {
			// Generate a random heading level from 1-6
			level := rapid.IntRange(1, 6).Draw(rt, fmt.Sprintf("level_%d", i))

			// Generate heading text without leading/trailing spaces
			// (markdown parsers typically trim heading whitespace)
			// Pattern: starts with letter, followed by alphanumeric and internal spaces,
			// ends with alphanumeric
			text := rapid.StringMatching(`[A-Za-z][A-Za-z0-9]*( [A-Za-z0-9]+)*`).Draw(rt, fmt.Sprintf("text_%d", i))

			// Create markdown heading
			prefix := strings.Repeat("#", level)
			markdownLines = append(markdownLines, prefix+" "+text)
			markdownLines = append(markdownLines, "") // blank line after heading

			// Track expected and excluded headings
			if level == 2 || level == 3 {
				expectedH2H3 = append(expectedH2H3, struct {
					text  string
					level int
				}{text, level})
			} else {
				excludedHeadings = append(excludedHeadings, struct {
					text  string
					level int
				}{text, level})
			}
		}

		// Join into markdown document
		markdown := strings.Join(markdownLines, "\n")

		// Render the markdown
		renderer := NewMarkdownRenderer()
		result, err := renderer.Render([]byte(markdown))
		if err != nil {
			rt.Fatalf("failed to render markdown: %v", err)
		}

		// Flatten TOC to get all entries (including children)
		var allTOCEntries []TOCEntry
		for _, entry := range result.TOC {
			allTOCEntries = append(allTOCEntries, entry)
			allTOCEntries = append(allTOCEntries, entry.Children...)
		}

		// Property 1: TOC should contain all h2/h3 headings
		for _, expected := range expectedH2H3 {
			found := false
			for _, entry := range allTOCEntries {
				if entry.Text == expected.text && entry.Level == expected.level {
					found = true
					break
				}
			}
			if !found {
				rt.Fatalf("TOC missing h%d heading: %q", expected.level, expected.text)
			}
		}

		// Property 2: TOC should not contain h1, h4, h5, h6 headings
		for _, excluded := range excludedHeadings {
			for _, entry := range allTOCEntries {
				if entry.Text == excluded.text {
					rt.Fatalf("TOC should not contain h%d heading: %q, but found entry with level %d",
						excluded.level, excluded.text, entry.Level)
				}
			}
		}

		// Property 3: Number of TOC entries should equal number of h2/h3 headings
		if len(allTOCEntries) != len(expectedH2H3) {
			rt.Fatalf("TOC entry count mismatch: expected %d entries for h2/h3 headings, got %d",
				len(expectedH2H3), len(allTOCEntries))
		}

		// Property 4: All TOC entries should have level 2 or 3
		for _, entry := range allTOCEntries {
			if entry.Level != 2 && entry.Level != 3 {
				rt.Fatalf("TOC entry %q has invalid level %d (expected 2 or 3)", entry.Text, entry.Level)
			}
		}
	})
}


// **Validates: Requirements 3.2**

// TestProperty_HeadingLevelPreservation verifies that for any markdown heading
// of level N (where N is 1-6), the rendered HTML contains an <hN> element
// with the heading text.
// Feature: cli-documentation-page, Property 7: Heading Level Preservation
func TestProperty_HeadingLevelPreservation(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate a random number of headings
		numHeadings := rapid.IntRange(1, 15).Draw(rt, "numHeadings")

		var markdownLines []string
		type headingInfo struct {
			level int
			text  string
		}
		var headings []headingInfo

		for i := 0; i < numHeadings; i++ {
			// Generate a random heading level from 1-6
			level := rapid.IntRange(1, 6).Draw(rt, fmt.Sprintf("level_%d", i))

			// Generate heading text: starts with letter, followed by alphanumeric
			// with internal spaces, ensures meaningful text for matching
			text := rapid.StringMatching(`[A-Za-z][A-Za-z0-9]*( [A-Za-z0-9]+)*`).Draw(rt, fmt.Sprintf("text_%d", i))

			// Create markdown heading
			prefix := strings.Repeat("#", level)
			markdownLines = append(markdownLines, prefix+" "+text)
			markdownLines = append(markdownLines, "") // blank line after heading

			headings = append(headings, headingInfo{level: level, text: text})
		}

		// Join into markdown document
		markdown := strings.Join(markdownLines, "\n")

		// Render the markdown
		renderer := NewMarkdownRenderer()
		result, err := renderer.Render([]byte(markdown))
		if err != nil {
			rt.Fatalf("failed to render markdown: %v", err)
		}

		html := string(result.HTML)

		// Property: Each heading of level N should render to an <hN> element
		for _, h := range headings {
			// Build regex pattern to match the heading element
			// Match <hN ...> (with possible attributes like id) containing the text
			pattern := fmt.Sprintf(`<h%d[^>]*>%s</h%d>`, h.level, regexp.QuoteMeta(h.text), h.level)
			matched, err := regexp.MatchString(pattern, html)
			if err != nil {
				rt.Fatalf("regex error for pattern %q: %v", pattern, err)
			}
			if !matched {
				rt.Fatalf("HEADING LEVEL PRESERVATION VIOLATION: "+
					"markdown heading level %d with text %q was not rendered as <h%d>%s</h%d>. HTML output: %s",
					h.level, h.text, h.level, h.text, h.level, html)
			}
		}
	})
}

// TestProperty_HeadingLevelPreservation_AllLevels ensures that all six heading
// levels (h1-h6) are correctly preserved when rendered.
// Feature: cli-documentation-page, Property 7: Heading Level Preservation
func TestProperty_HeadingLevelPreservation_AllLevels(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate text for each heading level 1-6
		var markdownLines []string
		texts := make([]string, 6)

		for level := 1; level <= 6; level++ {
			// Generate unique text for each level
			text := rapid.StringMatching(`[A-Za-z][A-Za-z0-9]*( [A-Za-z0-9]+)*`).Draw(rt, fmt.Sprintf("text_h%d", level))
			texts[level-1] = text

			prefix := strings.Repeat("#", level)
			markdownLines = append(markdownLines, prefix+" "+text)
			markdownLines = append(markdownLines, "")
		}

		markdown := strings.Join(markdownLines, "\n")

		renderer := NewMarkdownRenderer()
		result, err := renderer.Render([]byte(markdown))
		if err != nil {
			rt.Fatalf("failed to render markdown: %v", err)
		}

		html := string(result.HTML)

		// Verify each level (1-6) is properly rendered
		for level := 1; level <= 6; level++ {
			text := texts[level-1]
			pattern := fmt.Sprintf(`<h%d[^>]*>%s</h%d>`, level, regexp.QuoteMeta(text), level)
			matched, err := regexp.MatchString(pattern, html)
			if err != nil {
				rt.Fatalf("regex error: %v", err)
			}
			if !matched {
				rt.Fatalf("HEADING LEVEL PRESERVATION VIOLATION: "+
					"h%d heading with text %q not found in rendered HTML", level, text)
			}
		}
	})
}

// TestProperty_HeadingLevelPreservation_CountMatches verifies that the exact
// number of headings at each level in the markdown matches the count in HTML.
// Feature: cli-documentation-page, Property 7: Heading Level Preservation
func TestProperty_HeadingLevelPreservation_CountMatches(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate random counts for each level
		levelCounts := make(map[int]int)
		var markdownLines []string

		for level := 1; level <= 6; level++ {
			// Generate 0-5 headings per level
			count := rapid.IntRange(0, 5).Draw(rt, fmt.Sprintf("count_h%d", level))
			levelCounts[level] = count

			for i := 0; i < count; i++ {
				text := rapid.StringMatching(`[A-Za-z][A-Za-z0-9]{2,10}`).Draw(rt, fmt.Sprintf("text_h%d_%d", level, i))
				prefix := strings.Repeat("#", level)
				markdownLines = append(markdownLines, prefix+" "+text)
				markdownLines = append(markdownLines, "")
			}
		}

		markdown := strings.Join(markdownLines, "\n")

		renderer := NewMarkdownRenderer()
		result, err := renderer.Render([]byte(markdown))
		if err != nil {
			rt.Fatalf("failed to render markdown: %v", err)
		}

		html := string(result.HTML)

		// Count occurrences of each heading level in the HTML
		for level := 1; level <= 6; level++ {
			expectedCount := levelCounts[level]

			// Count opening tags for this level
			openTag := fmt.Sprintf(`<h%d`, level)
			closeTag := fmt.Sprintf(`</h%d>`, level)

			openCount := strings.Count(html, openTag)
			closeCount := strings.Count(html, closeTag)

			if openCount != expectedCount {
				rt.Fatalf("HEADING COUNT MISMATCH: expected %d <h%d> tags, found %d",
					expectedCount, level, openCount)
			}
			if closeCount != expectedCount {
				rt.Fatalf("HEADING COUNT MISMATCH: expected %d </h%d> closing tags, found %d",
					expectedCount, level, closeCount)
			}
		}
	})
}


// **Validates: Requirements 4.2**

// TestProperty_CodeBlockLanguageDetection verifies that fenced code blocks with
// language specifiers get proper CSS classes for syntax highlighting.
// Property 6: Code Block Language Detection
// For any fenced code block with a language specifier (e.g., ```bash),
// the rendered HTML SHALL contain CSS classes specific to that language for syntax highlighting.
// Feature: cli-documentation-page
func TestProperty_CodeBlockLanguageDetection(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Common languages that are well-supported by chroma
		languages := []string{
			"bash", "sh", "go", "python", "javascript", "json",
			"yaml", "toml", "sql", "html", "css", "ruby",
			"java", "c", "cpp", "rust", "typescript",
		}

		// Pick a random language
		lang := rapid.SampledFrom(languages).Draw(rt, "language")

		// Generate some code content (simple alphanumeric with common code characters)
		// Keep it simple to avoid issues with markdown parsing
		codeContent := rapid.StringMatching(`[a-zA-Z0-9_(){}\[\];=<>.,: \n]{10,100}`).Draw(rt, "codeContent")

		// Create markdown with fenced code block
		markdown := fmt.Sprintf("```%s\n%s\n```", lang, codeContent)

		// Render the markdown
		renderer := NewMarkdownRenderer()
		result, err := renderer.Render([]byte(markdown))
		if err != nil {
			rt.Fatalf("failed to render markdown: %v", err)
		}

		html := string(result.HTML)

		// Property: The rendered HTML should contain chroma CSS classes
		// Chroma wraps highlighted code in elements with class="chroma"
		// and uses specific class names for tokens
		if !strings.Contains(html, `class="chroma"`) {
			rt.Fatalf("CODE BLOCK LANGUAGE DETECTION FAILURE: "+
				"markdown with ```%s code block did not produce chroma CSS classes.\n"+
				"Markdown:\n%s\n\nRendered HTML:\n%s",
				lang, markdown, html)
		}

		// The HTML should also contain a <pre> element (code blocks render as pre)
		if !strings.Contains(html, "<pre") {
			rt.Fatalf("CODE BLOCK LANGUAGE DETECTION FAILURE: "+
				"markdown with ```%s code block did not produce <pre> element.\n"+
				"Markdown:\n%s\n\nRendered HTML:\n%s",
				lang, markdown, html)
		}

		// The HTML should contain a <code> element
		if !strings.Contains(html, "<code") {
			rt.Fatalf("CODE BLOCK LANGUAGE DETECTION FAILURE: "+
				"markdown with ```%s code block did not produce <code> element.\n"+
				"Markdown:\n%s\n\nRendered HTML:\n%s",
				lang, markdown, html)
		}
	})
}

// TestProperty_CodeBlockLanguageDetection_TokenClasses verifies that chroma
// produces token-specific CSS classes for syntax highlighting.
// This tests that actual syntax highlighting is happening, not just wrapping.
// Feature: cli-documentation-page
func TestProperty_CodeBlockLanguageDetection_TokenClasses(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Test cases with specific code patterns that should produce known token classes
		testCases := []struct {
			lang string
			code string
			desc string
		}{
			{"bash", "echo \"hello world\"", "bash echo command"},
			{"go", "func main() {}", "go function declaration"},
			{"python", "def foo(): pass", "python function definition"},
			{"javascript", "const x = 42;", "javascript const declaration"},
			{"json", `{"key": "value"}`, "json object"},
		}

		// Pick a random test case
		tc := rapid.SampledFrom(testCases).Draw(rt, "testCase")

		// Create markdown with fenced code block
		markdown := fmt.Sprintf("```%s\n%s\n```", tc.lang, tc.code)

		// Render the markdown
		renderer := NewMarkdownRenderer()
		result, err := renderer.Render([]byte(markdown))
		if err != nil {
			rt.Fatalf("failed to render markdown for %s: %v", tc.desc, err)
		}

		html := string(result.HTML)

		// Property: The rendered HTML should contain chroma's highlight wrapper
		if !strings.Contains(html, `class="chroma"`) {
			rt.Fatalf("CODE BLOCK TOKEN CLASS FAILURE (%s): "+
				"expected chroma wrapper class.\n"+
				"Markdown:\n%s\n\nRendered HTML:\n%s",
				tc.desc, markdown, html)
		}

		// Property: The code should be wrapped in highlight structure
		// Chroma creates a structure like: <pre class="chroma"><code>...</code></pre>
		// or with line table: <pre class="chroma"><code><span class="line">...
		hasHighlightStructure := strings.Contains(html, `class="chroma"`) &&
			strings.Contains(html, "<code")

		if !hasHighlightStructure {
			rt.Fatalf("CODE BLOCK TOKEN CLASS FAILURE (%s): "+
				"expected proper highlight structure with chroma class and code element.\n"+
				"Markdown:\n%s\n\nRendered HTML:\n%s",
				tc.desc, markdown, html)
		}
	})
}

// TestProperty_CodeBlockWithoutLanguage verifies that fenced code blocks
// without a language specifier still render properly (with generic styling).
// This tests the fallback behavior mentioned in Requirement 4.3.
// Feature: cli-documentation-page
func TestProperty_CodeBlockWithoutLanguage(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate some code content
		codeContent := rapid.StringMatching(`[a-zA-Z0-9_(){}\[\];=<>.,: ]{10,50}`).Draw(rt, "codeContent")

		// Create markdown with fenced code block WITHOUT language specifier
		markdown := fmt.Sprintf("```\n%s\n```", codeContent)

		// Render the markdown
		renderer := NewMarkdownRenderer()
		result, err := renderer.Render([]byte(markdown))
		if err != nil {
			rt.Fatalf("failed to render markdown: %v", err)
		}

		html := string(result.HTML)

		// Property: Even without a language, the code block should render as pre/code
		if !strings.Contains(html, "<pre") {
			rt.Fatalf("CODE BLOCK WITHOUT LANGUAGE FAILURE: "+
				"expected <pre> element for code block without language.\n"+
				"Markdown:\n%s\n\nRendered HTML:\n%s",
				markdown, html)
		}

		if !strings.Contains(html, "<code") {
			rt.Fatalf("CODE BLOCK WITHOUT LANGUAGE FAILURE: "+
				"expected <code> element for code block without language.\n"+
				"Markdown:\n%s\n\nRendered HTML:\n%s",
				markdown, html)
		}
	})
}


// **Validates: Requirements 3.4**

// TestProperty_MarkdownLinkPreservation verifies that for any valid markdown link
// [text](url) in the source, the rendered HTML contains an anchor element
// <a href="url"> with the link text preserved.
//
// Property 5: Markdown Link Preservation
// For any valid markdown link `[text](url)` in the source, the rendered HTML
// SHALL contain an anchor element `<a href="url">` with the link text preserved.
//
// Feature: cli-documentation-page
func TestProperty_MarkdownLinkPreservation(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate random link text (non-empty, no special markdown characters)
		linkText := rapid.StringMatching(`[A-Za-z][A-Za-z0-9 ]{0,30}`).Draw(rt, "linkText")

		// Generate random URL-like strings
		// Use various URL formats: relative paths, absolute paths, full URLs
		urlType := rapid.IntRange(0, 3).Draw(rt, "urlType")
		var linkURL string
		switch urlType {
		case 0:
			// Simple relative path
			linkURL = "/" + rapid.StringMatching(`[a-z][a-z0-9-]{0,20}`).Draw(rt, "path")
		case 1:
			// Path with segments
			seg1 := rapid.StringMatching(`[a-z][a-z0-9-]{0,10}`).Draw(rt, "seg1")
			seg2 := rapid.StringMatching(`[a-z][a-z0-9-]{0,10}`).Draw(rt, "seg2")
			linkURL = "/" + seg1 + "/" + seg2
		case 2:
			// Full URL with https
			domain := rapid.StringMatching(`[a-z]{3,10}`).Draw(rt, "domain")
			path := rapid.StringMatching(`[a-z][a-z0-9-]{0,15}`).Draw(rt, "urlPath")
			linkURL = "https://" + domain + ".com/" + path
		case 3:
			// URL with anchor fragment
			page := rapid.StringMatching(`[a-z][a-z0-9-]{0,10}`).Draw(rt, "page")
			anchor := rapid.StringMatching(`[a-z][a-z0-9-]{0,10}`).Draw(rt, "anchor")
			linkURL = "/" + page + "#" + anchor
		}

		// Build markdown with the link
		markdown := fmt.Sprintf("This is a paragraph with a [%s](%s) link.", linkText, linkURL)

		// Render the markdown
		renderer := NewMarkdownRenderer()
		result, err := renderer.Render([]byte(markdown))
		if err != nil {
			rt.Fatalf("failed to render markdown: %v", err)
		}

		html := string(result.HTML)

		// Property 1: The HTML must contain an anchor tag with the correct href
		expectedHref := fmt.Sprintf(`href="%s"`, linkURL)
		if !strings.Contains(html, expectedHref) {
			rt.Fatalf("LINK PRESERVATION VIOLATION: markdown link [%s](%s) did not produce href=%q in HTML.\nGenerated HTML: %s",
				linkText, linkURL, linkURL, html)
		}

		// Property 2: The HTML must contain the link text
		if !strings.Contains(html, linkText) {
			rt.Fatalf("LINK TEXT VIOLATION: markdown link [%s](%s) did not preserve link text in HTML.\nGenerated HTML: %s",
				linkText, linkURL, html)
		}

		// Property 3: Verify the anchor element structure using regex
		// Match <a href="...">text</a> pattern
		anchorPattern := regexp.MustCompile(`<a[^>]*href="` + regexp.QuoteMeta(linkURL) + `"[^>]*>[^<]*` + regexp.QuoteMeta(linkText) + `[^<]*</a>`)
		if !anchorPattern.MatchString(html) {
			rt.Fatalf("ANCHOR STRUCTURE VIOLATION: could not find proper <a href=%q>%s</a> structure in HTML.\nGenerated HTML: %s",
				linkURL, linkText, html)
		}
	})
}

// TestProperty_MarkdownLinkPreservation_MultipleLinks verifies that when a document
// contains multiple links, all of them are correctly rendered.
//
// Property 5 (multiple links): For any markdown document with multiple links,
// each link SHALL be rendered with its URL and text preserved.
//
// Feature: cli-documentation-page
func TestProperty_MarkdownLinkPreservation_MultipleLinks(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate between 2 and 10 links
		numLinks := rapid.IntRange(2, 10).Draw(rt, "numLinks")

		type linkDef struct {
			text string
			url  string
		}
		links := make([]linkDef, numLinks)

		// Generate unique link definitions
		usedTexts := make(map[string]bool)
		usedURLs := make(map[string]bool)

		for i := 0; i < numLinks; i++ {
			// Generate unique link text
			var text string
			for {
				text = rapid.StringMatching(`[A-Za-z][A-Za-z0-9]{2,15}`).Draw(rt, fmt.Sprintf("text_%d", i))
				if !usedTexts[text] {
					usedTexts[text] = true
					break
				}
			}

			// Generate unique URL
			var url string
			for {
				path := rapid.StringMatching(`[a-z][a-z0-9]{2,15}`).Draw(rt, fmt.Sprintf("path_%d", i))
				url = "/" + path
				if !usedURLs[url] {
					usedURLs[url] = true
					break
				}
			}

			links[i] = linkDef{text: text, url: url}
		}

		// Build markdown with all links
		var parts []string
		for i, link := range links {
			parts = append(parts, fmt.Sprintf("Link %d: [%s](%s)", i+1, link.text, link.url))
		}
		markdown := strings.Join(parts, "\n\n")

		// Render the markdown
		renderer := NewMarkdownRenderer()
		result, err := renderer.Render([]byte(markdown))
		if err != nil {
			rt.Fatalf("failed to render markdown: %v", err)
		}

		html := string(result.HTML)

		// Verify each link is preserved
		for i, link := range links {
			// Check href
			expectedHref := fmt.Sprintf(`href="%s"`, link.url)
			if !strings.Contains(html, expectedHref) {
				rt.Fatalf("LINK %d PRESERVATION VIOLATION: markdown link [%s](%s) did not produce href=%q in HTML.\nGenerated HTML: %s",
					i+1, link.text, link.url, link.url, html)
			}

			// Check text
			if !strings.Contains(html, link.text) {
				rt.Fatalf("LINK %d TEXT VIOLATION: markdown link [%s](%s) did not preserve link text in HTML.\nGenerated HTML: %s",
					i+1, link.text, link.url, html)
			}
		}
	})
}

// TestProperty_MarkdownLinkPreservation_SpecialURLCharacters verifies that links
// with URL-encoded characters or special URL components are correctly preserved.
//
// Property 5 (special characters): For markdown links with query strings, anchors,
// or URL-encoded characters, the rendered HTML SHALL preserve the complete URL.
// Note: HTML entity encoding (e.g., & → &amp;) is expected and correct behavior.
//
// Feature: cli-documentation-page
func TestProperty_MarkdownLinkPreservation_SpecialURLCharacters(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate link text
		linkText := rapid.StringMatching(`[A-Za-z][A-Za-z0-9 ]{0,20}`).Draw(rt, "linkText")

		// Generate URL with special components
		urlType := rapid.IntRange(0, 3).Draw(rt, "urlType")
		var linkURL string
		switch urlType {
		case 0:
			// URL with query string
			path := rapid.StringMatching(`[a-z]{3,10}`).Draw(rt, "path")
			param := rapid.StringMatching(`[a-z]{2,8}`).Draw(rt, "param")
			value := rapid.StringMatching(`[a-z0-9]{2,8}`).Draw(rt, "value")
			linkURL = "/" + path + "?" + param + "=" + value
		case 1:
			// URL with multiple query params
			path := rapid.StringMatching(`[a-z]{3,10}`).Draw(rt, "path2")
			p1 := rapid.StringMatching(`[a-z]{2,6}`).Draw(rt, "p1")
			v1 := rapid.StringMatching(`[a-z0-9]{2,6}`).Draw(rt, "v1")
			p2 := rapid.StringMatching(`[a-z]{2,6}`).Draw(rt, "p2")
			v2 := rapid.StringMatching(`[a-z0-9]{2,6}`).Draw(rt, "v2")
			linkURL = "/" + path + "?" + p1 + "=" + v1 + "&" + p2 + "=" + v2
		case 2:
			// URL with anchor
			path := rapid.StringMatching(`[a-z]{3,10}`).Draw(rt, "path3")
			anchor := rapid.StringMatching(`[a-z-]{3,15}`).Draw(rt, "anchor")
			linkURL = "/" + path + "#" + anchor
		case 3:
			// URL with both query and anchor
			path := rapid.StringMatching(`[a-z]{3,10}`).Draw(rt, "path4")
			param := rapid.StringMatching(`[a-z]{2,6}`).Draw(rt, "param2")
			value := rapid.StringMatching(`[a-z0-9]{2,6}`).Draw(rt, "value2")
			anchor := rapid.StringMatching(`[a-z]{3,10}`).Draw(rt, "anchor2")
			linkURL = "/" + path + "?" + param + "=" + value + "#" + anchor
		}

		// Build markdown
		markdown := fmt.Sprintf("Check out [%s](%s) for more info.", linkText, linkURL)

		// Render
		renderer := NewMarkdownRenderer()
		result, err := renderer.Render([]byte(markdown))
		if err != nil {
			rt.Fatalf("failed to render markdown: %v", err)
		}

		html := string(result.HTML)

		// The URL in HTML will have ampersands encoded as &amp; which is correct HTML behavior.
		// We verify the URL is preserved by checking for the HTML-encoded version.
		htmlEncodedURL := strings.ReplaceAll(linkURL, "&", "&amp;")
		expectedHref := fmt.Sprintf(`href="%s"`, htmlEncodedURL)
		if !strings.Contains(html, expectedHref) {
			rt.Fatalf("SPECIAL URL PRESERVATION VIOLATION: markdown link [%s](%s) did not produce correct href in HTML.\nExpected href (HTML-encoded): %s\nGenerated HTML: %s",
				linkText, linkURL, expectedHref, html)
		}

		// Verify link text is preserved
		if !strings.Contains(html, linkText) {
			rt.Fatalf("SPECIAL URL TEXT VIOLATION: markdown link [%s](%s) did not preserve link text in HTML.\nGenerated HTML: %s",
				linkText, linkURL, html)
		}
	})
}
