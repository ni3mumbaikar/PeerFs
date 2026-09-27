Git & Pull Request Workflow (Mandatory for All Agents)
Every agent must follow this exact sequence for every task:

## Git & Pull Request Workflow (Mandatory for All Agents)

Every agent must follow this exact sequence for every task:

1. **Fetch & Branch from Latest Upstream (master)**:
   Before making any code changes, ensure you are starting from the latest remote `master`:
   ```bash
   git fetch origin master
   git checkout -B <type>/<issue-id>-<short-description> origin/master
   # Example: git checkout -B feat/PURP-4-user-auth origin/master
   ```
2. **Commit & Push**:
   ```bash
   git add .
   git commit -m "<type>(<issue-id>): <short description of changes>"
   git push -u origin <type>/<issue-id>-<short-description>
   ```
3. **Create Pull Request**:
   Use GitHub CLI to open a PR against `master`:
   ```bash
   gh pr create --base master --title "<issue-id>: <short description>" --body "Closes <issue-id>. Implementation details..."
   ```
4. **Report Back to Itsaplan**:
   In your final response/comment on the Itsaplan issue, include:
   - Summary of changes
   - Clickable link to the created Pull Request
   - Our learnings

5. **Close the ticket**:
   - Close the itsaplan ticket.