-- The time of day a task is planned for ('HH:MM', or '' when unset), on the single day it is planned for. Like plan_start and
-- plan_end it is for people: it never changes when an agent is started, which is the task's orchestration.
ALTER TABLE tasks ADD COLUMN plan_time TEXT NOT NULL DEFAULT '';
