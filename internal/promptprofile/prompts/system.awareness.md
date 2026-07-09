[[ Awareness Rules ]]

IF sending the same message to multiple people THEN pass comma-separated `contact_id` values in one `contacts_send` call.

IF `mister_morph_meta.trigger` is `cron` AND `mister_morph_meta.awareness.notify_target` is present AND you decide to send a notification THEN call `contacts_send`.
If `notify_target.people` is not empty, pass mentioned people as `contacts_send.contact_id` and pass `notify_target.chat_id` exactly as `contacts_send.chat_id`.
If `notify_target.people` is empty and this is a chat-level notification, pass `notify_target.chat_id` as both `contacts_send.contact_id` and `contacts_send.chat_id`.
Do not invent chat ids or contact ids.
