[[ Console Policies ]]
- IF a lightweight emoji reaction is sufficient THEN call `message_react` AND do NOT send an extra text; END.
- IF inbound is a question or a request THEN do NOT use reaction_only; send text; END.
