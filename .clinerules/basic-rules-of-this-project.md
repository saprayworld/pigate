# Basic Rules

- ให้ทำความเข้าใจเอกสารใน docs/ ก่อนเริ่มดำเนินการทำงาน โดยเฉพาะ tech_stack_design.md

## NEVER Read or Analyze
- NEVER read .env files (.env, .env.local, .env.production, etc.)
- NEVER read files containing secrets, API keys, or passwords
- NEVER read files with names like: secrets, private, keys, config
- NEVER read database connection strings or credentials

## NEVER Suggest or Expose
- NEVER suggest committing secrets to git
- NEVER suggest exposing secrets in client-side code
- NEVER suggest logging secrets or environment variables
- NEVER suggest hardcoding secrets in code

## ALWAYS Use Placeholders
- ALWAY use placeholder values like: DATABASE_URL="mysql://user:pass@localhost/db"
- ALWAY use placeholder keys like: SECRET_KEY="your-secret-here"
- ALWAY use placeholder secrets like: AUTH_SECRET="your-secret-here"

## File Exclusions
- Exclude all .env* filrs from AI context
- Exclude files in /secrets, /private, /config directories
- Exclude files with extensions: .key, .pem, .p12

## Code Review Rules
- Flag any hardcoded in code reviews
- Suggest using environment variables instead of hardcoding
- Remind about .gitignore for sensitive files

## Privacy First
- User privacy and security comes first
- When in dobt, ask user before accessing sensitive files
- Never assume it's safe to read configuration files

## Other
- รีวิวช่องโหว่ของโค้ดที่เขียนเป็นระยะ เช่น Auth Bypass, SQL Injection, CORS, etc.
- ใช้ yarn แทน npm สำหรับโปรเจค NextJS, React หรือ NodeJS
- ไม่ต้องทำ Git commit จนกว่าผู้ใช้จะบอกให้ทำ
