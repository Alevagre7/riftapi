package domain

// Set is a group of Cards released together as a single product. The
// embedded CardCount, TCGPlayerID, CardmarketID, and PublishedOn
// fields are nullable because the gallery does not provide them.
type Set struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	SetID        string   `json:"set_id"`
	CardCount    *int     `json:"card_count"`
	TCGPlayerID  *string  `json:"tcgplayer_id"`
	CardmarketID *[]string `json:"cardmarket_id"`
	PublishedOn  *string  `json:"published_on"`
}
