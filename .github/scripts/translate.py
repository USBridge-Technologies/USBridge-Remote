import os
from openai import OpenAI

LANGUAGES = {
    'ES': 'Spanish',
    'JA': 'Japanese',
    'KO': 'Korean',
    'ZH': 'Simplified Chinese'
}

client = OpenAI(api_key=os.environ.get("OPENAI_API_KEY"))

def translate_readme():
    if not os.path.exists("README.md"):
        print("README.md not found!")
        return

    os.makedirs("docs", exist_ok=True)

    with open("README.md", "r", encoding="utf-8") as f:
        source_text = f.read()

    for lang_code, lang_name in LANGUAGES.items():
        print(f"Translating to {lang_name}...")
        prompt = f"""You are a professional technical translator specializing in software documentation and GitHub repositories.
Translate the following Markdown documentation into {lang_name}.

Rules:
1. Maintain all Markdown formatting, HTML tags, badging, code blocks, links, and layout structure intact.
2. DO NOT translate code blocks, terminal commands, URLs, image paths, or technical terms like 'USBridge Remote', 'KVM', 'Btrfs', 'Wayland', 'Tailscale'.
3. Output ONLY the translated Markdown text without any conversational intro or conclusion.

Text to translate:
{source_text}"""

        response = client.chat.completions.create(
            model="gpt-4o-mini",
            messages=[{"role": "user", "content": prompt}],
            temperature=0.3,
        )

        translated_text = response.choices[0].message.content

        output_filename = os.path.join("docs", f"README_{lang_code}.md")
        with open(output_filename, "w", encoding="utf-8") as f:
            f.write(translated_text)
        print(f"Saved {output_filename}")

if __name__ == "__main__":
    translate_readme()
