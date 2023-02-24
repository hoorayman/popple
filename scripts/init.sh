#!/usr/bin/env bash

git init
cp ./githooks/pre-commit .git/hooks/pre-commit
chmod 777 .git/hooks/pre-commit
echo 'git hooks init done.'
exit 0