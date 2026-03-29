# QA Scenario Creator - Claude Code Skill

A custom Claude Code skill for creating YAML scenario files for the BoostedBudget qa data populator framework through an interactive, question-driven workflow.

## What This Skill Does

This skill helps you create new test data scenarios by:
- 🎯 Guiding you through interactive questions
- ✅ Validating inputs against the schema
- 📝 Generating properly formatted YAML files
- 🔢 Auto-numbering scenario files
- 🚀 Providing ready-to-run commands

## Installation

### Option 1: Install to Claude Code Skills Directory (Recommended)

1. **Copy the skill folder** to your Claude Code skills directory:
   ```bash
   # Create skills directory if it doesn't exist
   mkdir -p ~/.config/claude-code/skills

   # Copy this skill
   cp -r .claude-skills/qa-scenario-creator ~/.config/claude-code/skills/
   ```

2. **Enable the skill** in Claude Code:
   - Open Claude Code settings
   - Navigate to **Settings > Capabilities > Skills**
   - Enable "QA Scenario Creator"

3. **Restart Claude Code** (if necessary)

### Option 2: Use In-Place (For Development)

You can also use the skill directly from the `.claude-skills/` directory in this repository. Claude Code will automatically discover it if the directory structure is correct.

## Usage

### Invoking the Skill

The skill activates when you use phrases like:
- "Create a new staging scenario"
- "Generate a scenario for testing pagination"
- "I need a scenario for edge case testing"
- "Add a new staging data configuration"

### Interactive Workflow

Once invoked, the skill will ask you questions about:

1. **Scenario metadata**: Name, description, category, tags
2. **User configuration**: Email, subscription tier
3. **Linked items**: Number of banks/institutions
4. **Accounts**: Types, names, balances (tier-appropriate)
5. **Transactions**: Auto-generated or manual (specific)
6. **Assertions**: What should be validated after population

### Example Session

```
You: "Create a new staging scenario for testing pagination"

Claude: I'll help you create a staging scenario. Let me ask you a few questions:

1. What should we call this scenario?
   → "UI Testing - Transaction Pagination"

2. What's the purpose of this scenario? (1-2 sentences)
   → "Test pagination with large transaction dataset"

3. What category does this fit?
   - demo
   - ui-testing ✓
   - edge-case
   - tier-limit
   - integration

4. Which subscription tier?
   - free
   - premium ✓
   - premium_plus

5. How many accounts do you need? (premium allows up to 30)
   → 5

6. For transaction generation, do you want:
   - Auto mode (generate random transactions) ✓
   - Manual mode (define specific transactions)

7. How many transactions per account?
   → 200 (1000 total)

[Generates 06-ui-testing-transaction-pagination.yaml]

✅ Scenario created successfully!

To run this scenario:
cd backend/scripts/qa-data-populator
OVERRIDE_USER_EMAIL="your-email@example.com" OVERRIDE_USER_MODE="existing" \
  ./populate.sh scenarios/06-ui-testing-transaction-pagination.yaml
```

## Files

- **Skill.md**: Main skill file with interactive workflow instructions
- **REFERENCE.md**: Complete schema reference and examples
- **README.md**: This file - installation and usage guide

## Skill Capabilities

### Account Types
- `checking` - Checking accounts
- `savings` - Savings accounts
- `credit_card` - Credit cards
- `loan` - Loans (student, auto, personal)
- `investment` - Investment accounts (brokerage, 401k)

### Subscription Tiers
- `free` - 3 accounts max, 30-day history
- `premium` - 30 accounts max, 365-day history
- `premium_plus` - Unlimited accounts, 730-day history

### Transaction Modes
- **Auto**: Generate random realistic transactions (great for volume)
- **Manual**: Define specific transactions (great for edge cases)

### Categories
- Demo data scenarios
- UI testing (pagination, performance)
- Edge cases (pending, refunds, special conditions)
- Tier limit testing
- Integration testing

## Tips for Best Results

1. **Be specific in your request**: "Create a scenario for testing refunds" works better than just "create a scenario"

2. **Use tier-appropriate accounts**:
   - Free: 3 accounts
   - Premium: 5-10 accounts
   - Premium+: 10+ accounts

3. **Choose the right transaction mode**:
   - Auto for large datasets (>50 transactions)
   - Manual for specific test cases

4. **Add meaningful assertions**: Always validate transaction count and account count at minimum

## Troubleshooting

### Skill Not Activating

**Problem**: Claude doesn't recognize your request

**Solutions**:
- Use keywords: "staging", "scenario", "create", "generate"
- Be specific: "I need a scenario for X"
- Check Settings > Capabilities > Skills to ensure skill is enabled

### Invalid YAML Generated

**Problem**: Generated YAML has schema errors

**Solutions**:
- Verify account types are valid (checking, savings, credit_card, loan, investment)
- Check tier limits (free: ≤3 accounts, premium: ≤30, premium+: unlimited)
- Ensure transaction mode has correct fields (auto: count_per_account, manual: list)

### File Already Exists

**Problem**: Scenario number conflicts with existing file

**Solutions**:
- Skill auto-numbers to avoid conflicts
- If manual numbering needed, check `backend/scripts/qa-data-populator/scenarios/` first
- Use next available number (currently 01-05 exist, so start at 06)

## Examples

### Quick Example: Simple Demo
```
Request: "Create a demo scenario for free tier users"

Result: 3 accounts, 15 transactions per account (auto mode)
File: 06-demo-free-tier-simple.yaml
```

### Quick Example: Performance Testing
```
Request: "Create a scenario to test pagination performance"

Result: 10 accounts, 100 transactions per account (1000 total, auto mode)
File: 07-ui-testing-pagination-performance.yaml
```

### Quick Example: Edge Cases
```
Request: "Create a scenario for testing pending transactions and refunds"

Result: 2 accounts, 5 manual transactions (pending, refunds, user notes)
File: 08-edge-case-pending-refunds.yaml
```

## Related Documentation

- **Skill.md**: Detailed workflow and instructions
- **REFERENCE.md**: Complete schema reference with examples
- **QA Data Populator README**: `backend/scripts/qa-data-populator/README.md`
- **Scenario Examples**: `backend/scripts/qa-data-populator/scenarios/`

## Development

### Modifying the Skill

1. Edit `Skill.md` to change the workflow or instructions
2. Edit `REFERENCE.md` to update schema documentation
3. Test changes by invoking the skill in Claude Code
4. Submit PR if changes benefit the team

### Adding Examples

Add new examples to `REFERENCE.md` under "Complete Examples" section.

## Support

- **GitHub Issues**: Report bugs or request features at [Issue #344](https://github.com/BoostedBudget/BoostedBudget/issues/344)
- **Documentation**: See `backend/scripts/qa-data-populator/README.md`
- **Example Scenarios**: `backend/scripts/qa-data-populator/scenarios/`

## Version

- **Version**: 1.0.0
- **Created**: 2025-11-13
- **Related PR**: #343 (QA Data Populator Framework)
- **Related Issue**: #342

---

🤖 Generated with [Claude Code](https://claude.com/claude-code)
