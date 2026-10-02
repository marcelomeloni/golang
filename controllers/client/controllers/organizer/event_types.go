package organizer
import "bilheteria-api/services/eventservice"
// ─── Tipos de request dos handlers de evento ─────────────────────────────────

type SaveDraftRequest struct {
	Title        string                             `json:"title"`
	Description  string                             `json:"description"`
	Instagram    string                             `json:"instagram"`
	Category     string                             `json:"category"`
	Location     *EventLocationInput                `json:"location"`
	StartDate    string                             `json:"start_date"`
	EndDate      string                             `json:"end_date"`
	Categories   []eventservice.TicketCategoryInput `json:"ticket_categories"`
	Requirements *EventRequirementsInput            `json:"requirements"`
}

type UpdateEventRequest struct {
	Title        *string                            `json:"title"`
	Description  *string                            `json:"description"`
	Instagram    *string                            `json:"instagram"`
	Category     *string                            `json:"category"`
	Location     *EventLocationInput                `json:"location"`
	StartDate    *string                            `json:"start_date"`
	EndDate      *string                            `json:"end_date"`
	Categories   []eventservice.TicketCategoryInput `json:"ticket_categories"`
	Requirements *EventRequirementsInput            `json:"requirements"`
}

type EventLocationInput struct {
	VenueName    string `json:"venue_name"`
	CEP          string `json:"cep"`
	Street       string `json:"street"`
	Number       string `json:"number"`
	Complement   string `json:"complement"`
	Neighborhood string `json:"neighborhood"`
	City         string `json:"city"`
	State        string `json:"state"`
}

type EventRequirementsInput struct {
	MinAge        string   `json:"min_age"`
	RequiredDocs  []string `json:"required_docs"`
	AcceptedTerms bool     `json:"accepted_terms"`
}