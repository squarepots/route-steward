from pathlib import Path

replacements = {
    'internal/steward/profile_routing_test.go': {
        '"  - DOMAIN-SUFFIX,example.net,DIRECT\\n"': '"  - \'DOMAIN-SUFFIX,example.net,DIRECT\'\\n"',
        '"  - GEOSITE,example-category,RST-Route-route-a\\n"': '"  - \'GEOSITE,example-category,RST-Route-route-a\'\\n"',
        '"  - GEOIP,US,RST-Route-route-a,no-resolve\\n"': '"  - \'GEOIP,US,RST-Route-route-a,no-resolve\'\\n"',
        '"  - PROCESS-NAME,launcher.exe,Applications\\n"': '"  - \'PROCESS-NAME,launcher.exe,Applications\'\\n"',
    },
    'internal/steward/steward_test.go': {
        '"  - PROCESS-NAME,com.example.app,Applications\\n"': '"  - \'PROCESS-NAME,com.example.app,Applications\'\\n"',
        '"  - PROCESS-NAME,launcher.exe,Applications\\n"': '"  - \'PROCESS-NAME,launcher.exe,Applications\'\\n"',
        '"  - GEOSITE,CN,DIRECT\\n"': '"  - \'GEOSITE,CN,DIRECT\'\\n"',
    },
    'internal/steward/migration_test.go': {
        '"GEOSITE,example-category,RST-Route-"+result.ReplacementRoute': '"\'GEOSITE,example-category,RST-Route-"+result.ReplacementRoute+"\'"',
    },
}
for path, items in replacements.items():
    p = Path(path)
    text = p.read_text(encoding='utf-8')
    for old, new in items.items():
        if old not in text:
            raise SystemExit(f'{path}: missing expected text {old}')
        text = text.replace(old, new)
    p.write_text(text, encoding='utf-8', newline='\n')
