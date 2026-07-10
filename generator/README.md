# Generator dokumentów — 360biuro

Serwer WWW (Go) z formularzem, który generuje komplet dokumentów w PDF
(`umowa`, `regulamin`, `cennik`, `umowa_powierzenia`) na podstawie plików
markdown w `pliki/`. Pola formularza są tworzone automatycznie na podstawie
zmiennych `{{ ... }}` znalezionych w dokumentach — po dodaniu nowej zmiennej
w markdownie, pole samo pojawi się w formularzu.

Każdy wygenerowany PDF ma w nagłówku wersję dokumentów (wpisywaną w
formularzu) oraz automatyczną datę i godzinę wygenerowania, a w stopce każdej
strony numerację i tę samą wersję/datę.

## Budowanie obrazu

```bash
docker build -t 360biuro-generator .
```

## Uruchomienie

Uruchom kontener, montując katalog `pliki/` z repozytorium (tak, aby zawsze
używać aktualnej wersji dokumentów z GitHuba):

```bash
docker run --rm -p 8080:8080 \
  -v "$(pwd)/pliki:/app/pliki:ro" \
  360biuro-generator
```

Następnie otwórz [http://localhost:8080](http://localhost:8080), wypełnij
formularz (w tym numer wersji dokumentów) i kliknij „Generuj dokumenty
(PDF)” — pobierze się plik ZIP z czterema PDF-ami.

### Opcjonalnie: domyślna wersja

Jeśli w katalogu `pliki/` (lub katalogu nadrzędnym) umieścisz plik `VERSION`
z numerem wersji, formularz automatycznie go podpowie jako wartość
domyślną pola „Wersja dokumentów”.

## Zmienne środowiskowe

| Zmienna    | Domyślnie    | Opis                                      |
| ---------- | ------------ | ------------------------------------------ |
| `PORT`     | `8080`       | Port, na którym nasłuchuje serwer.        |
| `DOCS_DIR` | `/app/pliki` | Katalog z plikami `.md` do wygenerowania. |
