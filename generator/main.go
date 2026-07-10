// Generator dokumentów 360biuro.
//
// Serwer WWW, który:
//  1. skanuje pliki markdown w katalogu DOCS_DIR pod kątem zmiennych postaci
//     {{ nazwa_zmiennej }} i na ich podstawie buduje formularz HTML,
//  2. po wypełnieniu formularza podmienia zmienne w dokumentach (umowa,
//     regulamin, cennik, umowa powierzenia), renderuje je do PDF ze stopką
//     zawierającą wersję dokumentów i datę wygenerowania, i zwraca komplet
//     jako archiwum ZIP.
package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

// docSpec opisuje jeden dokument źródłowy generowany do PDF.
type docSpec struct {
	file  string // nazwa pliku .md w katalogu dokumentów
	title string // używana w nazwie pliku PDF
}

// docs to lista dokumentów wchodzących w skład generowanego kompletu,
// w kolejności w jakiej trafią do archiwum ZIP.
var docs = []docSpec{
	{"umowa.md", "umowa"},
	{"regulamin.md", "regulamin"},
	{"cennik.md", "cennik"},
	{"umowa_powierzenia.md", "umowa_powierzenia"},
}

// placeholderRe dopasowuje zmienne postaci {{ nazwa }} (także z odwrotnym
// ukośnikiem maskującym podkreślnik, np. {{ uslugi\_start }}).
var placeholderRe = regexp.MustCompile(`\{\{\s*([^}]+?)\s*\}\}`)

// mdEscapeRe dopasowuje znaki specjalne markdown, które trzeba zamaskować
// w wartościach wpisanych przez użytkownika, aby nie zepsuły struktury
// dokumentu (tabel, pogrubień, linków) po podstawieniu.
var mdEscapeRe = regexp.MustCompile("([\\\\`*_{}\\[\\]|])")

func docsDir() string {
	if d := os.Getenv("DOCS_DIR"); d != "" {
		return d
	}
	return "./pliki"
}

// normalizeVarName usuwa odwrotne ukośniki używane w źródłowych plikach
// markdown do maskowania podkreślnika (np. "uslugi\_start" -> "uslugi_start").
func normalizeVarName(raw string) string {
	return strings.ReplaceAll(strings.TrimSpace(raw), `\`, "")
}

// escapeMarkdown maskuje znaki specjalne markdown w wartości wpisanej przez
// użytkownika, tak aby po podstawieniu do dokumentu nie zmieniły jego
// struktury (np. podkreślnik w nazwie firmy nie włączy kursywy).
func escapeMarkdown(v string) string {
	return mdEscapeRe.ReplaceAllString(v, `\$1`)
}

// discoverVariables skanuje wszystkie dokumenty i zwraca posortowaną,
// unikalną listę nazw zmiennych {{ ... }} użytych w dokumentach.
func discoverVariables() ([]string, error) {
	seen := map[string]bool{}
	for _, d := range docs {
		content, err := os.ReadFile(filepath.Join(docsDir(), d.file))
		if err != nil {
			return nil, fmt.Errorf("nie mogę odczytać %s: %w", d.file, err)
		}
		for _, m := range placeholderRe.FindAllStringSubmatch(string(content), -1) {
			seen[normalizeVarName(m[1])] = true
		}
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		if n != "" {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names, nil
}

// humanLabel zamienia nazwę zmiennej na czytelną etykietę pola formularza,
// np. "biuro_email_opiekun" -> "Biuro email opiekun".
func humanLabel(name string) string {
	label := strings.ReplaceAll(name, "_", " ")
	if label == "" {
		return label
	}
	return strings.ToUpper(label[:1]) + label[1:]
}

// groupOf przypisuje zmienną do sekcji formularza na podstawie prefiksu.
func groupOf(name string) string {
	switch {
	case strings.HasPrefix(name, "biuro_"):
		return "Biuro"
	case strings.HasPrefix(name, "klient_"):
		return "Klient"
	default:
		return "Pozostałe"
	}
}

type formField struct {
	Name  string
	Label string
}

type formGroup struct {
	Name   string
	Fields []formField
}

// buildFormGroups grupuje listę zmiennych w sekcje formularza w ustalonej
// kolejności: Biuro, Klient, Pozostałe.
func buildFormGroups(vars []string) []formGroup {
	order := []string{"Biuro", "Klient", "Pozostałe"}
	byGroup := map[string][]formField{}
	for _, v := range vars {
		g := groupOf(v)
		byGroup[g] = append(byGroup[g], formField{Name: v, Label: humanLabel(v)})
	}
	result := make([]formGroup, 0, len(order))
	for _, g := range order {
		if fields, ok := byGroup[g]; ok {
			result = append(result, formGroup{Name: g, Fields: fields})
		}
	}
	return result
}

// defaultVersion próbuje odczytać domyślną wersję z pliku VERSION,
// zmiennej środowiskowej lub skrótu commita git, jeśli jest dostępny.
func defaultVersion() string {
	if v := strings.TrimSpace(os.Getenv("DOCUMENT_VERSION")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("VERSION")); v != "" {
		return v
	}
	candidates := []string{
		filepath.Join(docsDir(), "VERSION"),
		filepath.Join(docsDir(), "..", "VERSION"),
	}
	for _, p := range candidates {
		if b, err := os.ReadFile(p); err == nil {
			return strings.TrimSpace(string(b))
		}
	}
	if _, err := exec.LookPath("git"); err == nil {
		if out, err := exec.Command("git", "rev-parse", "--short", "HEAD").CombinedOutput(); err == nil {
			if v := strings.TrimSpace(string(out)); v != "" {
				return v
			}
		}
	}
	return ""
}

func loadOfficeProfiles() (map[string]map[string]string, error) {
	candidates := []string{
		os.Getenv("OFFICE_PROFILES_PATH"),
		"./biura.json",
		"./generator/biura.json",
		filepath.Join("..", "biura.json"),
	}
	for _, p := range candidates {
		if p == "" {
			continue
		}
		if b, err := os.ReadFile(p); err == nil {
			profiles := map[string]map[string]string{}
			if err := json.Unmarshal(b, &profiles); err != nil {
				return nil, fmt.Errorf("niepoprawny format %s: %w", p, err)
			}
			return profiles, nil
		}
	}
	return map[string]map[string]string{}, nil
}

func officeProfileNames(profiles map[string]map[string]string) []string {
	names := make([]string, 0, len(profiles))
	for name := range profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

type indexPageData struct {
	Groups              []formGroup
	OfficeProfileNames  []string
	OfficeProfilesJSON  template.JS
}

var indexTmpl = template.Must(template.New("index").Parse(`<!doctype html>
<html lang="pl">
<head>
<meta charset="utf-8">
<title>Generator dokumentów — 360biuro</title>
<style>
  :root { color-scheme: light; }
  body { font-family: -apple-system, "Segoe UI", Roboto, Arial, sans-serif; background: #f4f5f7; margin: 0; padding: 40px 20px; color: #222; }
  .wrap { max-width: 760px; margin: 0 auto; background: #fff; border-radius: 10px; padding: 32px 40px 40px; box-shadow: 0 1px 4px rgba(0,0,0,.08); }
  h1 { font-size: 22px; margin-bottom: 4px; }
  p.sub { color: #666; margin-top: 0; margin-bottom: 28px; font-size: 14px; }
  fieldset { border: 1px solid #e2e2e6; border-radius: 8px; margin-bottom: 20px; padding: 16px 20px 20px; }
  legend { font-weight: 600; padding: 0 8px; font-size: 14px; }
  label { display: block; font-size: 13px; color: #444; margin-top: 12px; margin-bottom: 4px; }
  input[type=text], select { width: 100%; box-sizing: border-box; padding: 8px 10px; border: 1px solid #d0d0d6; border-radius: 6px; font-size: 14px; }
  input[type=text]:focus, select:focus { outline: 2px solid #4a7dfc; border-color: transparent; }
  button { margin-top: 24px; background: #2555d9; color: #fff; border: none; padding: 12px 26px; font-size: 15px; border-radius: 7px; cursor: pointer; }
  button:hover { background: #1c45b8; }
  .hint { font-size: 12px; color: #888; margin-top: 6px; }
</style>
</head>
<body>
<div class="wrap">
  <h1>Generator dokumentów księgowych</h1>
  <p class="sub">Wypełnij dane, aby wygenerować komplet dokumentów PDF: umowę, regulamin, cennik i umowę powierzenia. Puste pola pojawią się w dokumentach jako "……………".</p>
  <form method="POST" action="/generate">
    <fieldset>
      <legend>Biuro</legend>
      <label for="office-profile-select">Wybierz profil biura</label>
      <select id="office-profile-select" name="_biuro_profile">
        {{ range .OfficeProfileNames }}
        <option value="{{ . }}">{{ . }}</option>
        {{ end }}
      </select>
      <p class="hint">Zmiana profilu uzupełni pola formularza z sekcji „Biuro”.</p>
    </fieldset>
    <script type="application/json" id="office-profiles">{{ .OfficeProfilesJSON }}</script>
    {{ range .Groups }}
    <fieldset>
      <legend>{{ .Name }}</legend>
      {{ range .Fields }}
      <label for="{{ .Name }}">{{ .Label }}</label>
      <input type="text" id="{{ .Name }}" name="{{ .Name }}">
      {{ end }}
    </fieldset>
    {{ end }}
    <button type="submit">Generuj dokumenty (PDF)</button>
  </form>
</div>
<script>
(function () {
  const profilesEl = document.getElementById('office-profiles');
  if (!profilesEl) return;
  const profiles = JSON.parse(profilesEl.textContent || '{}');
  const select = document.getElementById('office-profile-select');
  const officeInputs = Array.from(document.querySelectorAll('input[name^="biuro_"]'));
  if (!select || officeInputs.length === 0) return;
  function applyProfile() {
    const profileName = select.value;
    const profile = profileName && profiles[profileName] ? profiles[profileName] : {};
    officeInputs.forEach(function (input) {
      input.value = profile[input.name] || '';
    });
  }
  select.addEventListener('change', applyProfile);
  applyProfile();
})();
</script>
</body>
</html>`))

func indexHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	vars, err := discoverVariables()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	profiles, err := loadOfficeProfiles()
	if err != nil {
		log.Printf("warning: %v", err)
	}
	profilesJSON, err := json.Marshal(profiles)
	if err != nil {
		profilesJSON = []byte("{}")
	}
	data := indexPageData{
		Groups:             buildFormGroups(vars),
		OfficeProfileNames: officeProfileNames(profiles),
		OfficeProfilesJSON: template.JS(profilesJSON),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := indexTmpl.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// substitute podmienia w treści markdown wszystkie wystąpienia {{ zmienna }}
// na wartość podaną przez użytkownika (lub widoczny placeholder, gdy brak
// wartości).
func substitute(content string, values map[string]string) string {
	return placeholderRe.ReplaceAllStringFunc(content, func(m string) string {
		sub := placeholderRe.FindStringSubmatch(m)
		name := normalizeVarName(sub[1])
		if v, ok := values[name]; ok && v != "" {
			return v
		}
		return "……………"
	})
}

// mdToHTML renderuje markdown (z obsługą tabel GFM) do HTML.
func mdToHTML(md string) (string, error) {
	gm := goldmark.New(goldmark.WithExtensions(extension.GFM))
	var buf bytes.Buffer
	if err := gm.Convert([]byte(md), &buf); err != nil {
		return "", err
	}
	return buf.String(), nil
}

type docPageData struct {
	Version     string
	GeneratedAt string
	Body        template.HTML
}

var docTmpl = template.Must(template.New("doc").Parse(`<!doctype html>
<html lang="pl">
<head>
<meta charset="utf-8">
<style>
  body { font-family: "DejaVu Sans", Arial, sans-serif; font-size: 11pt; line-height: 1.5; color: #1a1a1a; }
  h1 { font-size: 18pt; margin-bottom: 4px; }
  h2 { font-size: 13pt; margin-top: 22px; border-bottom: 1px solid #ccc; padding-bottom: 4px; }
  h3 { font-size: 11.5pt; margin-top: 16px; }
  table { border-collapse: collapse; width: 100%; margin: 10px 0; }
  th, td { border: 1px solid #999; padding: 5px 8px; font-size: 10pt; text-align: left; }
  th { background: #f0f0f0; }
  .meta { font-size: 9pt; color: #555; margin: 6px 0 18px 0; padding: 6px 10px; background: #f6f6f6; border-left: 3px solid #888; }
  ol { padding-left: 22px; }
  a { color: #1a4fa0; }
</style>
</head>
<body>
<div class="meta">{{ if .Version }}Wersja dokumentów: {{ .Version }} &nbsp;|&nbsp;{{ end }} Wygenerowano: {{ .GeneratedAt }}</div>
{{ .Body }}
</body>
</html>`))

func renderHTML(bodyHTML, version, generatedAt string) (string, error) {
	var buf bytes.Buffer
	err := docTmpl.Execute(&buf, docPageData{
		Version:     version,
		GeneratedAt: generatedAt,
		Body:        template.HTML(bodyHTML), //nolint:gosec // treść pochodzi z naszego renderu goldmark
	})
	return buf.String(), err
}

// htmlToPDF generuje PDF z HTML za pomocą wkhtmltopdf uruchamianego pod
// xvfb-run (wersja wkhtmltopdf z repozytoriów Debiana wymaga wirtualnego
// serwera X do renderowania w trybie headless).
func htmlToPDF(html, footerHTMLContent string) ([]byte, error) {
	tmpDir, err := os.MkdirTemp("", "docgen-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	htmlPath := filepath.Join(tmpDir, "doc.html")
	footerPath := filepath.Join(tmpDir, "footer.html")
	pdfPath := filepath.Join(tmpDir, "doc.pdf")
	if err := os.WriteFile(htmlPath, []byte(html), 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(footerPath, []byte(footerHTMLContent), 0o644); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	args := []string{
		"-a", "wkhtmltopdf",
		"--quiet",
		"--page-size", "A4",
		"--margin-top", "18mm",
		"--margin-bottom", "18mm",
		"--margin-left", "18mm",
		"--margin-right", "18mm",
		"--encoding", "utf-8",
		"--footer-spacing", "4",
		"--footer-html", footerPath,
		htmlPath,
		pdfPath,
	}
	cmd := exec.CommandContext(ctx, "xvfb-run", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("wkhtmltopdf: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}
	return os.ReadFile(pdfPath)
}

// sanitizeVersion usuwa znaki niebezpieczne w nazwach plików.
func sanitizeVersion(v string) string {
	repl := strings.NewReplacer(" ", "_", "/", "-", "\\", "-")
	v = repl.Replace(v)
	if v == "" {
		v = "brak"
	}
	return v
}

func footerHTML(version, generatedAt string) string {
	left := fmt.Sprintf("Wygenerowano: %s", generatedAt)
	if strings.TrimSpace(version) != "" {
		left = fmt.Sprintf("Wersja: %s — %s", version, generatedAt)
	}
	return fmt.Sprintf(`<!doctype html>
<html>
<head>
<meta charset="utf-8">
<style>
  body { margin: 0; padding: 0; font-family: "DejaVu Sans", Arial, sans-serif; font-size: 8px; color: #555; }
  .wrap { display: flex; justify-content: space-between; align-items: center; width: 100%; }
  a { color: #1a4fa0; text-decoration: none; }
</style>
</head>
<body>
<div class="wrap">
  <span>%s</span>
  <span>Strona [page]/[topage] · <a href="https://www.360biuro.pl">www.360biuro.pl</a></span>
</div>
</body>
</html>`, left)
}

func generateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	version := strings.TrimSpace(defaultVersion())
	generatedAt := time.Now().Format("2006-01-02 15:04")

	values := map[string]string{}
	for key, vals := range r.Form {
		if strings.HasPrefix(key, "_") || len(vals) == 0 {
			continue
		}
		values[key] = escapeMarkdown(strings.TrimSpace(vals[0]))
	}

	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	versionSlug := sanitizeVersion(version)

	for i, d := range docs {
		raw, err := os.ReadFile(filepath.Join(docsDir(), d.file))
		if err != nil {
			http.Error(w, fmt.Sprintf("nie mogę odczytać %s: %v", d.file, err), http.StatusInternalServerError)
			return
		}
		filled := substitute(string(raw), values)
		bodyHTML, err := mdToHTML(filled)
		if err != nil {
			http.Error(w, fmt.Sprintf("błąd renderowania %s: %v", d.file, err), http.StatusInternalServerError)
			return
		}
		fullHTML, err := renderHTML(bodyHTML, version, generatedAt)
		if err != nil {
			http.Error(w, fmt.Sprintf("błąd szablonu %s: %v", d.file, err), http.StatusInternalServerError)
			return
		}
		pdf, err := htmlToPDF(
			fullHTML,
			footerHTML(version, generatedAt),
		)
		if err != nil {
			http.Error(w, fmt.Sprintf("błąd generowania PDF dla %s: %v", d.file, err), http.StatusInternalServerError)
			return
		}
		entryName := fmt.Sprintf("dokumenty_v%s/%02d_%s.pdf", versionSlug, i+1, d.title)
		fw, err := zw.Create(entryName)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if _, err := fw.Write(pdf); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if err := zw.Close(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	fname := fmt.Sprintf("dokumenty_360biuro_v%s_%s.zip", versionSlug, time.Now().Format("2006-01-02_1504"))
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+fname+`"`)
	w.Write(zipBuf.Bytes())
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/", indexHandler)
	mux.HandleFunc("/generate", generateHandler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Printf("Serwer wystartował: http://localhost:%s (katalog dokumentów: %s)", port, docsDir())
	log.Fatal(srv.ListenAndServe())
}
