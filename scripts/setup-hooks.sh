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

echo "Running tests before push..."

# Run tests
npm run test
TEST_RESULT=$?

if [ $TEST_RESULT -ne 0 ]; then
    echo "❌ Tests failed. Please fix them before pushing."
    exit 1
fi

echo "✅ All tests passed. Push allowed."
exit 0
HOOK

chmod +x .git/hooks/pre-push

echo "Git hooks setup complete!"
