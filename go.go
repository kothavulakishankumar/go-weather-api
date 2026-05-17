package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
)

// ─── Configuration ────────────────────────────────────────────────────────────

const (
	owmBaseURL  = "https://api.openweathermap.org/data/2.5/weather"
	defaultPort = "8080"
)

// ─── Request / Response structs ───────────────────────────────────────────────

// WeatherResponse is the clean JSON payload we return to callers.
type WeatherResponse struct {
	City        string  `json:"city"`
	Country     string  `json:"country"`
	Temperature float64 `json:"temperature_celsius"`
	FeelsLike   float64 `json:"feels_like_celsius"`
	TempMin     float64 `json:"temp_min_celsius"`
	TempMax     float64 `json:"temp_max_celsius"`
	Humidity    int     `json:"humidity_percent"`
	WindSpeed   float64 `json:"wind_speed_ms"`
	WindDeg     int     `json:"wind_direction_deg"`
	Condition   string  `json:"condition"`
	Description string  `json:"description"`
	Visibility  int     `json:"visibility_meters"`
	Cloudiness  int     `json:"cloudiness_percent"`
}

// ErrorResponse is returned on any failure.
type ErrorResponse struct {
	Error   string `json:"error"`
	Details string `json:"details,omitempty"`
}

// ─── OpenWeatherMap API response shape ────────────────────────────────────────

type owmResponse struct {
	Name string `json:"name"`
	Sys  struct {
		Country string `json:"country"`
	} `json:"sys"`
	Main struct {
		Temp      float64 `json:"temp"`
		FeelsLike float64 `json:"feels_like"`
		TempMin   float64 `json:"temp_min"`
		TempMax   float64 `json:"temp_max"`
		Humidity  int     `json:"humidity"`
	} `json:"main"`
	Wind struct {
		Speed float64 `json:"speed"`
		Deg   int     `json:"deg"`
	} `json:"wind"`
	Clouds struct {
		All int `json:"all"`
	} `json:"clouds"`
	Visibility int `json:"visibility"`
	Weather    []struct {
		Main        string `json:"main"`
		Description string `json:"description"`
	} `json:"weather"`
	Cod     interface{} `json:"cod"` // OWM returns int on success, string on error
	Message string      `json:"message"`
}

// ─── Handlers ─────────────────────────────────────────────────────────────────

// weatherHandler handles GET /weather?city=<name>
func weatherHandler(apiKey string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Only allow GET
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "Method not allowed", "")
			return
		}

		// Extract city from query param
		city := r.URL.Query().Get("city")
		if city == "" {
			writeError(w, http.StatusBadRequest, "Missing required query parameter: city", "")
			return
		}

		// Build OWM request URL
		params := url.Values{}
		params.Set("q", city)
		params.Set("appid", apiKey)
		params.Set("units", "metric") // Celsius
		reqURL := fmt.Sprintf("%s?%s", owmBaseURL, params.Encode())

		// Call OWM API
		resp, err := http.Get(reqURL)
		if err != nil {
			writeError(w, http.StatusBadGateway, "Failed to reach weather service", err.Error())
			return
		}
		defer resp.Body.Close()

		// Decode OWM response
		var owm owmResponse
		if err := json.NewDecoder(resp.Body).Decode(&owm); err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to parse weather data", err.Error())
			return
		}

		// OWM returns HTTP 200 even for "city not found" — check cod field
		if resp.StatusCode != http.StatusOK {
			details := owm.Message
			if details == "" {
				details = fmt.Sprintf("OWM returned status %d", resp.StatusCode)
			}
			writeError(w, http.StatusNotFound, "City not found", details)
			return
		}

		// Guard against empty weather slice
		condition, description := "Unknown", "No description available"
		if len(owm.Weather) > 0 {
			condition = owm.Weather[0].Main
			description = owm.Weather[0].Description
		}

		// Build clean response
		weather := WeatherResponse{
			City:        owm.Name,
			Country:     owm.Sys.Country,
			Temperature: owm.Main.Temp,
			FeelsLike:   owm.Main.FeelsLike,
			TempMin:     owm.Main.TempMin,
			TempMax:     owm.Main.TempMax,
			Humidity:    owm.Main.Humidity,
			WindSpeed:   owm.Wind.Speed,
			WindDeg:     owm.Wind.Deg,
			Condition:   condition,
			Description: description,
			Visibility:  owm.Visibility,
			Cloudiness:  owm.Clouds.All,
		}

		writeJSON(w, http.StatusOK, weather)
	}
}

// healthHandler handles GET /health
func healthHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"service": "go-weather-api",
		"version": "1.0.0",
	})
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Powered-By", "Go Weather API")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("ERROR encoding JSON response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, message, details string) {
	writeJSON(w, status, ErrorResponse{Error: message, Details: details})
}

// ─── Main ─────────────────────────────────────────────────────────────────────

func main() {
	// Read API key from environment variable
	apiKey := os.Getenv("OWM_API_KEY")
	if apiKey == "" {
		log.Fatal("OWM_API_KEY environment variable is not set.\n" +
			"Get your free key at https://openweathermap.org/api")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/weather", weatherHandler(apiKey))
	mux.HandleFunc("/health", healthHandler)

	// Catch-all for unknown routes
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			writeError(w, http.StatusNotFound, "Route not found", r.URL.Path)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"message":  "Go Weather API is running",
			"endpoint": "/weather?city={city_name}",
			"health":   "/health",
		})
	})

	addr := ":" + port
	log.Printf("🌤  Go Weather API listening on http://localhost%s", addr)
	log.Printf("📡  Endpoint: GET http://localhost%s/weather?city=London", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}