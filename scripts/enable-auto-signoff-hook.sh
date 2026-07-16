#!/usr/bin/env bash

set -euo pipefail

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || true)"
if [[ -z "$repo_root" ]]; then
    echo "Error: This script must be run inside a Git repository." >&2
    exit 1
fi

hook_path="$repo_root/.git/hooks/prepare-commit-msg"
mkdir -p "$(dirname "$hook_path")"

cat > "$hook_path" <<'EOF'
#!/usr/bin/env bash

set -euo pipefail

commit_message_file="${1:-}"
if [[ -z "$commit_message_file" || ! -f "$commit_message_file" ]]; then
    echo "Error: Commit message file is missing or does not exist." >&2
    exit 1
fi

name="$(git config user.name || true)"
email="$(git config user.email || true)"

if [[ -z "$name" || -z "$email" ]]; then
    echo "Error: Git user.name or user.email is not set." >&2
    echo "Hint: git config --global user.name \"Your Name\"" >&2
    echo "Hint: git config --global user.email \"you@example.com\"" >&2
    exit 1
fi

git interpret-trailers \
    --if-exists doNothing \
    --if-missing add \
    --trailer "Signed-off-by: $name <$email>" \
    --in-place \
    "$commit_message_file"
EOF

chmod +x "$hook_path"

echo "Installed hook: $hook_path"
echo "Auto sign-off is now enabled for this repository."
