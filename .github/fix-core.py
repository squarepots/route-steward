from pathlib import Path
p = Path('internal/steward/migration_test.go')
text = p.read_text(encoding='utf-8')
old = '''func TestMigrationUpdatesExplicitProfileServiceBindings(t *testing.T) {
	state, source, input := migrationFixture(t, "direct", false)
	profile := findProfile(state.Inventory, "primary")
	profile.Routing = &ProfileRouting{ServiceRoutes: []ProfileServiceRoute{{Service: "openai", Route: source.ID}}}
	if err := state.Save(false); err != nil {
		t.Fatal(err)
	}
	result, err := migrateRouteWith(context.Background(), state, source.ID, input, migrationTestDependencies(nil, "healthy"))
	if err != nil || result.Status != "complete" {
		t.Fatalf("migration did not complete: %#v err=%v", result, err)
	}
	profile = findProfile(state.Inventory, "primary")
	if profile == nil || profile.Routing == nil || len(profile.Routing.ServiceRoutes) != 1 || profile.Routing.ServiceRoutes[0].Route != result.ReplacementRoute {
		t.Fatalf("explicit service binding did not follow replacement Route: %#v", profile)
	}
	artifact, err := os.ReadFile(filepath.Join(state.Inventory.Delivery.Directory, "desktop.yaml"))
	if err != nil || !strings.Contains(string(artifact), "GEOSITE,openai,RST-Route-"+result.ReplacementRoute) {
		t.Fatalf("migrated artifact did not use the replacement service selector: err=%v artifact=%s", err, artifact)
	}
}
'''
new = '''func TestMigrationUpdatesExplicitProfileRoutingRules(t *testing.T) {
	state, source, input := migrationFixture(t, "direct", false)
	profile := findProfile(state.Inventory, "primary")
	profile.Routing = &ProfileRouting{Rules: []ProfileRoutingRule{{Match: ProfileRoutingMatch{Type: "geosite", Value: "example-category"}, Action: ProfileRoutingAction{Type: "route", Route: source.ID}}}}
	if err := state.Save(false); err != nil {
		t.Fatal(err)
	}
	result, err := migrateRouteWith(context.Background(), state, source.ID, input, migrationTestDependencies(nil, "healthy"))
	if err != nil || result.Status != "complete" {
		t.Fatalf("migration did not complete: %#v err=%v", result, err)
	}
	profile = findProfile(state.Inventory, "primary")
	if profile == nil || profile.Routing == nil || len(profile.Routing.Rules) != 1 || profile.Routing.Rules[0].Action.Route != result.ReplacementRoute {
		t.Fatalf("explicit routing rule did not follow replacement Route: %#v", profile)
	}
	artifact, err := os.ReadFile(filepath.Join(state.Inventory.Delivery.Directory, "desktop.yaml"))
	if err != nil || !strings.Contains(string(artifact), "GEOSITE,example-category,RST-Route-"+result.ReplacementRoute) {
		t.Fatalf("migrated artifact did not use the replacement route selector: err=%v artifact=%s", err, artifact)
	}
}
'''
if old not in text:
    raise SystemExit('expected migration test block not found')
p.write_text(text.replace(old, new, 1), encoding='utf-8', newline='\n')
