from pathlib import Path
p = Path('internal/steward/steward_test.go')
text = p.read_text(encoding='utf-8')
old = 'import (\n\t"context"\n\t"encoding/json"\n'
new = 'import (\n\t"context"\n\t"encoding/json"\n\t"errors"\n'
if old not in text:
    raise SystemExit('steward_test import block not found')
text = text.replace(old, new, 1)
text = text.replace('strings.Repeat("a", 5120)', 'strings.Repeat("a", subscriptionSecretChunkBytes*subscriptionMaxChunks)', 1)
text = text.replace('size != 5120 {\n\t\tt.Fatal("5120-byte subscription body was rejected")', 'size != subscriptionSecretChunkBytes*subscriptionMaxChunks {\n\t\tt.Fatal("maximum chunked subscription body was rejected")', 1)
text = text.replace('strings.Repeat("é", 2560)', 'strings.Repeat("é", subscriptionSecretChunkBytes*subscriptionMaxChunks/2)', 1)
text = text.replace('size != 5120 {\n\t\tt.Fatal("multibyte subscription body was miscounted")', 'size != subscriptionSecretChunkBytes*subscriptionMaxChunks {\n\t\tt.Fatal("multibyte subscription body was miscounted")', 1)
text = text.replace('strings.Repeat("a", 5121)', 'strings.Repeat("a", subscriptionSecretChunkBytes*subscriptionMaxChunks+1)', 1)
text = text.replace('strings.Contains(string(encoded), "secret-bearing")', 'strings.Contains(string(encoded), "synthetic detail")', 1)
p.write_text(text, encoding='utf-8', newline='\n')
