import os
import re

def remove_russian_from_file(filepath):
    with open(filepath, 'r', encoding='utf-8') as f:
        lines = f.readlines()
    
    new_lines = []
    for line in lines:
        if re.search(r'[А-Яа-яЁё]', line):
            # If it's a comment line, just remove the russian part or skip
            if line.strip().startswith('#'):
                # just strip the russian characters and keep the rest or skip entirely
                # Let's just remove the comment line entirely if it has russian
                continue
            elif "echo" in line:
                # remove the whole echo line
                continue
            else:
                # remove russian characters from the line
                line = re.sub(r'[А-Яа-яЁё]', '', line)
                new_lines.append(line)
        else:
            new_lines.append(line)
            
    with open(filepath, 'w', encoding='utf-8') as f:
        f.writelines(new_lines)

for root, dirs, files in os.walk("scripts"):
    for file in files:
        if file.endswith(".sh"):
            remove_russian_from_file(os.path.join(root, file))

print("Russian text removed from scripts.")
