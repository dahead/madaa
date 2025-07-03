# MADAA - Mass Data Analysis Assistant

MADAA ist ein Kommandozeilen-Tool zur schnellen und umfassenden Analyse von Verzeichnisstrukturen.

## Features

- Detaillierte Dateityp-Analyse
- Identifizierung der größten Dateien
- Größenverteilung
- Altersanalyse von Dateien
- Erkennung spezieller Dateien (versteckt, System, Symlinks)
- Verzeichnisinformationen
- Fortschrittsanzeige während der Analyse
- Konfigurierbare Dateityp-Kategorien
- Parallele Verarbeitung für optimale Performance
- Farbkodierte Ausgabe für bessere Übersichtlichkeit

## Installation

```
bash go install github.com/dahead/madaa@latest
```

### Parameter

- `--count N`: Anzahl der anzuzeigenden Top-Einträge (Standard: 10)
- `<Verzeichnispfad>`: Zu analysierendes Verzeichnis


## Verwendung

```
$ madaa --count 5 /home/user/
```

### Ausgabe

Die Analyse zeigt:
- Übersicht der Gesamtdateien und -größen
- Top N häufigste Dateitypen
- Top N größte Dateien
- Top N Dateien pro Typ
- Größenverteilung (tiny bis large)
- Altersanalyse mit ältesten/neuesten Dateien
- Spezielle Dateiinformationen
- Verzeichnisstatistiken
