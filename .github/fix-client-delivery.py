from pathlib import Path
p = Path('internal/steward/steward_test.go')
text = p.read_text(encoding='utf-8')
old = 'import (\n\t"context"\n\t"encoding/json"\n'
new = 'import (\n\t"context"\n\t"encoding/json"\n\t"errors"\n'
if old not in text:
    raise SystemExit('steward_test import block not found')
p.write_text(text.replace(old, new, 1), encoding='utf-8', newline='\n')
