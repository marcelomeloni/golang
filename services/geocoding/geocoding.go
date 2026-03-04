package geocoding

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Coordinates representa um ponto geográfico.
type Coordinates struct {
	Lat float64
	Lng float64
}

var httpClient = &http.Client{Timeout: 5 * time.Second}

// FromCityState busca as coordenadas de uma cidade usando o Nominatim (OpenStreetMap).
// Retorna nil se não encontrar — o evento é salvo mesmo sem geolocalização.
func FromCityState(ctx context.Context, city, state string) (*Coordinates, error) {
	if city == "" {
		return nil, nil
	}

	query := city
	if state != "" {
		query = fmt.Sprintf("%s, %s, Brasil", city, state)
	}

	reqURL := fmt.Sprintf(
		"https://nominatim.openstreetmap.org/search?q=%s&format=json&limit=1&countrycodes=br",
		url.QueryEscape(query),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	// Nominatim exige User-Agent identificando a aplicação
	req.Header.Set("User-Agent", "reppy-organizador/1.0")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var results []struct {
		Lat string `json:"lat"`
		Lon string `json:"lon"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil || len(results) == 0 {
		return nil, nil // cidade não encontrada — não é erro fatal
	}

	lat, err := strconv.ParseFloat(results[0].Lat, 64)
	if err != nil {
		return nil, nil
	}
	lng, err := strconv.ParseFloat(results[0].Lon, 64)
	if err != nil {
		return nil, nil
	}

	return &Coordinates{Lat: lat, Lng: lng}, nil
}