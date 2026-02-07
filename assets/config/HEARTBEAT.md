# Heartbeat Checklist

## Contacts Proactive Check

- Use `memory_recently` first (`days=3`, `limit=20`) to load recent context and routing clues.
- Use `contacts_list` (`status=active`) to review active contacts.
- Rank current candidates with `contacts_candidate_rank` (`limit=3`) and pick top results.
- Send selected items using `contacts_send` (one send call per selected contact).
- After observing response/engagement, call `contacts_feedback_update` with `signal=positive|neutral|negative`.
- If no contact is selected, summarize the reason (for example: no fresh candidates, cooldown, trust constraints).
- If sending fails, summarize the error and next retry action.
