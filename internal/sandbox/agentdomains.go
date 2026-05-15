package sandbox

// loginDomainsFor returns the domains an agent needs reachable to complete
// its initial login / auth flow. They are offered (opt-in, prompted) at
// scaffold time so a deny-all sandbox is usable out of the box.
//
// Keep this list strictly login/API-essential. Analytics, telemetry, and
// crash-reporting endpoints must not be added here — those are the user's
// call.
func loginDomainsFor(agent string) []string {
	switch agent {
	case "claude":
		return []string{
			"api.anthropic.com",
			"downloads.claude.ai",
		}
	default:
		return nil
	}
}
