package cv

import (
	"fmt"
	"slices"
	"time"
)

// Language is a language a CV can be written in: the words New Leaf adds to
// its PDF and share page. To offer another language, add it to languages;
// the editor lists them on the Profile page. The editor itself is in
// English.
type Language struct {
	Code    string `json:"code"`    // ISO 639-1; also what Typst hyphenates by
	Name    string `json:"name"`    // in the language itself
	English string `json:"english"` // in English

	// For the share page; not sent to the editor.
	DownloadLabel string `json:"-"` // the button
	ExpiryNote    string `json:"-"` // the note under the CV; %s is the date

	present    string            // the end of an item that hasn't ended
	sections   map[string]string // section titles, by section
	months     [12]string        // for item dates: "Sep 2023"
	longMonths [12]string        // for the share page's expiry date
	longDate   string            // %[1]d the day, %[2]s the month, %[3]d the year
}

// MaxLangs is how many languages one CV can have: a main one and another.
const MaxLangs = 2

var Languages = []Language{
	{
		Code: "en", Name: "English", English: "English",
		present: "Present",
		sections: map[string]string{
			"experience": "Work experience", "education": "Education", "publications": "Publications",
			"output": "Other output", "presentations": "Presentations", "teaching": "Teaching",
			"awards": "Grants & awards", "extracurricular": "Extracurricular activities", "volunteering": "Volunteering",
		},
		months:        [12]string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"},
		longMonths:    [12]string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"},
		longDate:      "%[2]s %[1]d, %[3]d",
		DownloadLabel: "Download PDF",
		ExpiryNote:    "This link expires on %s.",
	},
	{
		Code: "nl", Name: "Nederlands", English: "Dutch",
		present: "heden",
		sections: map[string]string{
			"experience": "Werkervaring", "education": "Opleiding", "publications": "Publicaties",
			"output": "Overige output", "presentations": "Presentaties", "teaching": "Onderwijs",
			"awards": "Beurzen & prijzen", "extracurricular": "Extracurriculaire activiteiten", "volunteering": "Vrijwilligerswerk",
		},
		months:        [12]string{"jan", "feb", "mrt", "apr", "mei", "jun", "jul", "aug", "sep", "okt", "nov", "dec"},
		longMonths:    [12]string{"januari", "februari", "maart", "april", "mei", "juni", "juli", "augustus", "september", "oktober", "november", "december"},
		longDate:      "%[1]d %[2]s %[3]d",
		DownloadLabel: "Download pdf",
		ExpiryNote:    "Deze link verloopt op %s.",
	},
	{
		Code: "de", Name: "Deutsch", English: "German",
		present: "heute",
		sections: map[string]string{
			"experience": "Berufserfahrung", "education": "Ausbildung", "publications": "Publikationen",
			"output": "Weitere Beiträge", "presentations": "Vorträge", "teaching": "Lehre",
			"awards": "Stipendien & Auszeichnungen", "extracurricular": "Weitere Aktivitäten", "volunteering": "Ehrenamt",
		},
		months:        [12]string{"Jan.", "Feb.", "März", "Apr.", "Mai", "Juni", "Juli", "Aug.", "Sept.", "Okt.", "Nov.", "Dez."},
		longMonths:    [12]string{"Januar", "Februar", "März", "April", "Mai", "Juni", "Juli", "August", "September", "Oktober", "November", "Dezember"},
		longDate:      "%[1]d. %[2]s %[3]d",
		DownloadLabel: "PDF herunterladen",
		ExpiryNote:    "Dieser Link läuft am %s ab.",
	},
	{
		Code: "fr", Name: "Français", English: "French",
		present: "aujourd’hui",
		sections: map[string]string{
			"experience": "Expérience professionnelle", "education": "Formation", "publications": "Publications",
			"output": "Autres productions", "presentations": "Communications", "teaching": "Enseignement",
			"awards": "Bourses et prix", "extracurricular": "Activités extra-professionnelles", "volunteering": "Bénévolat",
		},
		months:        [12]string{"janv.", "févr.", "mars", "avr.", "mai", "juin", "juil.", "août", "sept.", "oct.", "nov.", "déc."},
		longMonths:    [12]string{"janvier", "février", "mars", "avril", "mai", "juin", "juillet", "août", "septembre", "octobre", "novembre", "décembre"},
		longDate:      "%[1]d %[2]s %[3]d",
		DownloadLabel: "Télécharger le PDF",
		ExpiryNote:    "Ce lien expire le %s.",
	},
	{
		Code: "es", Name: "Español", English: "Spanish",
		present: "actualidad",
		sections: map[string]string{
			"experience": "Experiencia laboral", "education": "Formación", "publications": "Publicaciones",
			"output": "Otros resultados", "presentations": "Presentaciones", "teaching": "Docencia",
			"awards": "Becas y premios", "extracurricular": "Actividades extracurriculares", "volunteering": "Voluntariado",
		},
		months:        [12]string{"ene", "feb", "mar", "abr", "may", "jun", "jul", "ago", "sept", "oct", "nov", "dic"},
		longMonths:    [12]string{"enero", "febrero", "marzo", "abril", "mayo", "junio", "julio", "agosto", "septiembre", "octubre", "noviembre", "diciembre"},
		longDate:      "%[1]d de %[2]s de %[3]d",
		DownloadLabel: "Descargar PDF",
		ExpiryNote:    "Este enlace caduca el %s.",
	},
	{
		Code: "it", Name: "Italiano", English: "Italian",
		present: "oggi",
		sections: map[string]string{
			"experience": "Esperienza professionale", "education": "Istruzione e formazione", "publications": "Pubblicazioni",
			"output": "Altri prodotti", "presentations": "Presentazioni", "teaching": "Didattica",
			"awards": "Borse e premi", "extracurricular": "Attività extracurricolari", "volunteering": "Volontariato",
		},
		months:        [12]string{"gen", "feb", "mar", "apr", "mag", "giu", "lug", "ago", "set", "ott", "nov", "dic"},
		longMonths:    [12]string{"gennaio", "febbraio", "marzo", "aprile", "maggio", "giugno", "luglio", "agosto", "settembre", "ottobre", "novembre", "dicembre"},
		longDate:      "%[1]d %[2]s %[3]d",
		DownloadLabel: "Scarica il PDF",
		ExpiryNote:    "Questo link scade il %s.",
	},
	{
		Code: "pt", Name: "Português", English: "Portuguese",
		present: "presente",
		sections: map[string]string{
			"experience": "Experiência profissional", "education": "Formação académica", "publications": "Publicações",
			"output": "Outros resultados", "presentations": "Apresentações", "teaching": "Docência",
			"awards": "Bolsas e prémios", "extracurricular": "Atividades extracurriculares", "volunteering": "Voluntariado",
		},
		months:        [12]string{"jan", "fev", "mar", "abr", "mai", "jun", "jul", "ago", "set", "out", "nov", "dez"},
		longMonths:    [12]string{"janeiro", "fevereiro", "março", "abril", "maio", "junho", "julho", "agosto", "setembro", "outubro", "novembro", "dezembro"},
		longDate:      "%[1]d de %[2]s de %[3]d",
		DownloadLabel: "Descarregar PDF",
		ExpiryNote:    "Esta ligação expira a %s.",
	},
}

// language looks a language up by its code; English if there is none.
func LanguageOf(code string) Language {
	if i := slices.IndexFunc(Languages, func(l Language) bool { return l.Code == code }); i >= 0 {
		return Languages[i]
	}
	return Languages[0]
}

func knownLang(code string) bool {
	return slices.ContainsFunc(Languages, func(l Language) bool { return l.Code == code })
}

// langCodes lists every language New Leaf knows.
func langCodes() []string {
	codes := make([]string, len(Languages))
	for i, l := range Languages {
		codes[i] = l.Code
	}
	return codes
}

// date writes a day out in full, as "2 januari 2026".
func (l Language) LongDate(t time.Time) string {
	return fmt.Sprintf(l.longDate, t.Day(), l.longMonths[t.Month()-1], t.Year())
}

// validLangs checks a CV's languages: at least one, at most MaxLangs, known
// and different.
func validLangs(langs []string) error {
	if len(langs) == 0 || len(langs) > MaxLangs {
		return fmt.Errorf("a CV has 1 to %d languages", MaxLangs)
	}
	for i, l := range langs {
		if !knownLang(l) {
			return fmt.Errorf("unknown language %q", l)
		}
		if slices.Contains(langs[:i], l) {
			return fmt.Errorf("%s twice", LanguageOf(l).English)
		}
	}
	return nil
}
