#!/bin/bash
echo "Setting up git hooks..."
mkdir -p .git/hooks

# Create pre-push hook
cat << 'HOOK' > .git/hooks/pre-push
#!/bin/bash

# Protect main branch from direct pushes
current_branch=$(git rev-parse --abbrev-ref HEAD)
if [ "$current_branch" = "main" ]; then
    echo "❌ Error: Direct push to main branch is not allowed."
    echo "Please create a new branch and submit a pull request."
    exit 1
fi

# No repo test suite — the repo is a plain dcode workspace (authored
# .deepagents/ surface + skills; no harness code). Secret scanning runs via
# the gitleaks pre-commit hook on commit.
echo "✅ Pre-push checks passed. Push allowed."
exit 0
HOOK

chmod +x .git/hooks/pre-push

echo "Git hooks setup complete!"
