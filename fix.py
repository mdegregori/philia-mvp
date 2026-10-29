import re

with open('main.go', 'r', encoding='utf-8') as f:
    content = f.read()

# Rimuove le parentesi di troppo e sistema l'indentazione di room-info
content = re.sub(
    r'(fmt\.Println\("=======================\\n"\))\s*\}\s*\}\s*case "room-info":',
    r'\1\n\n\t\tcase "room-info":',
    content
)

# Assicura che tutti i "case" siano allineati correttamente
content = re.sub(r'^\s{4,}case "', '\t\tcase "', content, flags=re.MULTILINE)

with open('main.go', 'w', encoding='utf-8') as f:
    f.write(content)

print("✅ main.go riparato con successo! Ora puoi eseguire: go build -o pep.exe .")