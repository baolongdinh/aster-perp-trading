package farming

// SymbolData represents symbol information
type SymbolData struct {
	Symbol     string  `json:"symbol"`
	BaseAsset  string  `json:"baseAsset"`
	QuoteAsset string  `json:"quoteAsset"`
	Status     string  `json:"status"`
	Volume24h  float64 `json:"volume24h"`
	Count24h   int     `json:"count24h"`
}
