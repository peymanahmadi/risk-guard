package rules

import (
	"context"
	"fmt"
	"math"

	"github.com/peymanahmadi/riskguard/pkg/riskguard"
)

const earthRadiusKm = 6371.0

// GeoVelocityRule implements "impossible travel" detection: if an entity's
// previous transaction was in one location and this one is far enough away
// that traveling between them within the elapsed time would require an
// implausible speed, the transaction is flagged as likely account takeover
// or credential sharing.
type GeoVelocityRule struct {
	Store riskguard.ProfileStore
	// MaxPlausibleSpeedKmh is the fastest speed considered ordinary travel
	// (defaults to 900 km/h, roughly commercial flight speed, if zero).
	MaxPlausibleSpeedKmh float64
}

func NewGeoVelocityRule(store riskguard.ProfileStore) *GeoVelocityRule {
	return &GeoVelocityRule{Store: store}
}

func (r *GeoVelocityRule) Name() string { return "geo_velocity" }

func (r *GeoVelocityRule) Evaluate(ctx context.Context, tx riskguard.Transaction) (riskguard.RuleResult, error) {
	profile, err := r.Store.GetProfile(ctx, tx.EntityID)
	if err != nil {
		return riskguard.RuleResult{}, fmt.Errorf("geo_velocity: get profile: %w", err)
	}

	if profile.LastSeenAt.IsZero() {
		// No prior location on record; nothing to compare against yet.
		return riskguard.RuleResult{Rule: r.Name(), Score: 0, Triggered: false}, nil
	}

	elapsed := tx.CreatedAt.Sub(profile.LastSeenAt).Hours()
	if elapsed <= 0 {
		// Clock skew or out-of-order delivery; don't penalize for it here.
		return riskguard.RuleResult{Rule: r.Name(), Score: 0, Triggered: false}, nil
	}

	distanceKm := haversineKm(profile.LastLat, profile.LastLon, tx.Lat, tx.Lon)
	requiredSpeed := distanceKm / elapsed

	maxSpeed := r.MaxPlausibleSpeedKmh
	if maxSpeed == 0 {
		maxSpeed = 900
	}

	if requiredSpeed <= maxSpeed {
		return riskguard.RuleResult{Rule: r.Name(), Score: 0, Triggered: false}, nil
	}

	// Score scales with how far past plausible the required speed is,
	// saturating quickly since "impossible" is a fairly binary signal.
	ratio := requiredSpeed / maxSpeed
	score := 50 + math.Min(ratio*10, 50)

	return riskguard.RuleResult{
		Rule:      r.Name(),
		Triggered: true,
		Score:     score,
		Severity:  riskguard.SeverityHigh,
		Reason: fmt.Sprintf("implied travel speed %.0f km/h between %s and %s (%.0f km in %.1fh)",
			requiredSpeed, profile.LastCountry, tx.Country, distanceKm, elapsed),
	}, nil
}

func haversineKm(lat1, lon1, lat2, lon2 float64) float64 {
	if lat1 == 0 && lon1 == 0 {
		return 0
	}
	rad := func(deg float64) float64 { return deg * math.Pi / 180 }
	dLat := rad(lat2 - lat1)
	dLon := rad(lon2 - lon1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(rad(lat1))*math.Cos(rad(lat2))*math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadiusKm * c
}
