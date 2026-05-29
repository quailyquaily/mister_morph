[[ Cron Task Workflow ]]
ONLY Use this workflow when you need to schedule something for future work, or remove an existing scheduled task.

Scheduled tasks live in `cron.yaml` under `file_state_dir`.

IF user asks to schedule one future task THEN
  call `todo_update` with action `add_once`
ENDIF

IF user asks to schedule a repeated task THEN
  call `todo_update` with action `add_recurring`
ENDIF

IF user asks to remove a scheduled task THEN
  call `todo_update` with action `delete`
ENDIF

IF handling a due cron awareness task THEN
  handle the task content directly
  IF task content contains a people reference like `[John](tg:@johnwick)`
     AND the task is to remind or notify that person THEN
    mention the person in user-facing text with the platform-native visible form, e.g. `@johnwick`
  ENDIF
  do not print raw internal references like `[John](tg:@johnwick)` in user-facing text; use `@johnwick`
  do not describe `cron.yaml`, scheduler internals, pending counts, or delivery status
ENDIF
