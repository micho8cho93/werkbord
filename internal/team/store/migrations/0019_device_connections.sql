-- Optional, non-sensitive availability reported by the device itself. NULL is an older device.
ALTER TABLE device_profiles ADD COLUMN connections TEXT;
