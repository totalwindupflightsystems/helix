package config

// Defaults returns a Config populated with safe, hardcoded default values.
// These are the lowest-precedence values in the 5-tier loading order
// (defaults → config.yaml → pricing.yaml → env vars → CLI flags).
func Defaults() *Config {
	return &Config{
		Version: 1,
		Forgejo: ForgejoConfig{
			URL:         "http://localhost:3030",
			InternalURL: "http://forgejo-helix:3000",
			AdminUser:   "helio",
		},
		Chimera: ChimeraConfig{
			URL:              "http://localhost:8765",
			InternalURL:      "http://chimera-helix:8765",
			DefaultFormation: "standard",
			ArbiterFormation: "arbiter",
			BudgetFormation:  "budget",
			Timeout:          "120s",
		},
		LangFuse: LangFuseConfig{
			URL:         "http://localhost:3001",
			InternalURL: "http://langfuse-helix:3000",
			Enabled:     true,
		},
		GitReins: GitReinsConfig{
			Model:         "deepseek-v4-flash",
			Provider:      "deepseek",
			MaxIterations: 25,
			MaxTime:       "5m",
			TestMode:      "diff",
		},
		Identity: IdentityConfig{
			KnownFriendsPath: "/opt/hermes-demo/.hermes/h4f/known-friends.json",
			SSHKeyDir:        "~/.helix/keys",
			StatePath:        "~/.helix/state.json",
		},
		Estimator: EstimatorConfig{
			PricingPath:        "~/.helix/pricing.yaml",
			CacheHitRatioPro:   0.60,
			CacheHitRatioFlash: 0.80,
			BudgetResetDay:     "sunday",
			BudgetResetTime:    "00:00 UTC",
		},
		Marketplace: MarketplaceConfig{
			RegistryPath:        "~/.helix/marketplace",
			AutoDeprecationDays: 30,
			TrustRecalcSchedule: "0 2 * * *",
		},
		Negotiation: NegotiationConfig{
			MaxRounds:     3,
			RoundTimeout:  "5m",
			GlobalTimeout: "30m",
			TranscriptDir: "~/.helix/negotiations",
		},
		Prompts: PromptsConfig{
			RegistryPath:        "prompts",
			DeprecatedGraceDays: 30,
			RetiredAfterDays:    180,
		},
		Services: ServicesConfig{
			Forgejo: ServiceTarget{
				URL:            "http://forgejo-helix:3000",
				HealthEndpoint: "/api/v1/version",
			},
			Chimera: ServiceTarget{
				URL:            "http://chimera-helix:8765",
				HealthEndpoint: "/v1/health",
			},
			LangFuse: ServiceTarget{
				URL:            "http://langfuse-helix:3000",
				HealthEndpoint: "/api/public/health",
			},
		},
		Budget: BudgetConfig{
			DefaultWeeklyUSD: BudgetTierConfig{
				Pro:   10.00,
				Flash: 5.00,
			},
			OveragePolicy:       "block",
			EscalationThreshold: 1.5,
		},
	}
}
