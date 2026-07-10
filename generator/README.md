# Generator dokumentów — 360biuro

Serwer WWW (Go) z formularzem, który generuje komplet dokumentów w PDF
(`umowa`, `regulamin`, `cennik`, `umowa_powierzenia`) na podstawie plików
markdown w `pliki/`. Pola formularza są tworzone automatycznie na podstawie
zmiennych `{{ ... }}` znalezionych w dokumentach — po dodaniu nowej zmiennej
w markdownie, pole samo pojawi się w formularzu.

Każdy wygenerowany PDF ma w nagłówku automatycznie dodaną wersję dokumentów
(commit/wersję z systemu, jeśli jest dostępna) oraz datę i godzinę
wygenerowania, a w stopce każdej strony numerację i link do strony
www.360biuro.pl.

## Budowanie obrazu

Z katalogu głównego repozytorium:

```bash
docker build -t 360biuro-generator generator/
```

## Uruchomienie

Z katalogu głównego repozytorium uruchom kontener, montując katalog `pliki/`
(tak, aby zawsze używać aktualnej wersji dokumentów z GitHuba):

```bash
docker run --rm -p 8080:8080 \
  -v "$(pwd)/pliki:/app/pliki:ro" \
  360biuro-generator
```

Następnie otwórz [http://localhost:8080](http://localhost:8080), wypełnij
formularz (w tym numer wersji dokumentów) i kliknij „Generuj dokumenty
(PDF)” — pobierze się plik ZIP z czterema PDF-ami.

### Profil biura i wersja dokumentów

Na formularzu pojawia się lista rozwijana z profilami biura z pliku
`biura.json`. Po wyborze profilu pola z sekcji „Biuro” są automatycznie
wypełniane danymi z wybranego profilu.

Jeśli w katalogu `pliki/` (lub katalogu nadrzędnym) umieścisz plik `VERSION`
z numerem wersji, albo ustawisz zmienną środowiskową `DOCUMENT_VERSION`,
serwer użyje jej jako wersji dokumentów w nagłówku/stopce PDF.

## Zmienne środowiskowe

| Zmienna    | Domyślnie | Opis                                                                             |
| ---------- | --------- | -------------------------------------------------------------------------------- |
| `PORT`               | `8080`    | Port, na którym nasłuchuje serwer.                                               |
| `DOCS_DIR`           | `./pliki` | Katalog z plikami `.md` do wygenerowania (w obrazie Dockera: `/app/pliki`).      |
| `DOCUMENT_VERSION`   | —         | Wersja dokumentów używana w PDF, jeśli nie ma pliku `VERSION`.                   |
| `OFFICE_PROFILES_PATH` | —         | Ścieżka do pliku JSON z profilami biura.                                         |

Przy uruchomieniu lokalnym (bez Dockera) wykonuj z katalogu `generator/`:

```bash
DOCS_DIR=../pliki go run .
```
