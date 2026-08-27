function text(value) {
  return String(value || "").trim();
}

function fieldNumber(raw, min, max) {
  if (!/^\d+$/.test(raw)) {
    throw new Error("invalid cron field");
  }
  const value = Number(raw);
  if (!Number.isInteger(value) || value < min || value > max) {
    throw new Error("invalid cron field");
  }
  return value;
}

function cronField(raw, min, max, sundayAlias = false) {
  const values = new Set();
  let any = false;
  for (const rawToken of text(raw).split(",")) {
    const token = text(rawToken);
    if (!token) {
      throw new Error("invalid cron field");
    }
    const stepParts = token.split("/");
    if (stepParts.length > 2) {
      throw new Error("invalid cron field");
    }
    const base = text(stepParts[0]);
    const step = stepParts.length === 2 ? fieldNumber(text(stepParts[1]), 1, Number.MAX_SAFE_INTEGER) : 1;
    let start;
    let end;
    if (base === "*") {
      start = min;
      end = max;
      any ||= step === 1;
    } else {
      const range = base.split("-");
      if (range.length > 2) {
        throw new Error("invalid cron field");
      }
      start = fieldNumber(text(range[0]), min, max);
      end = range.length === 2 ? fieldNumber(text(range[1]), min, max) : start;
      if (start > end) {
        throw new Error("invalid cron field");
      }
    }
    for (let value = start; value <= end; value += step) {
      values.add(sundayAlias && value === 7 ? 0 : value);
    }
  }
  return { any, values };
}

export function parseCronExpression(raw) {
  const parts = text(raw).split(/\s+/);
  if (parts.length !== 5) {
    throw new Error("invalid cron expression");
  }
  return {
    minute: cronField(parts[0], 0, 59),
    hour: cronField(parts[1], 0, 23),
    day: cronField(parts[2], 1, 31),
    month: cronField(parts[3], 1, 12),
    weekday: cronField(parts[4], 0, 7, true),
  };
}

export function isValidCronExpression(raw) {
  try {
    parseCronExpression(raw);
    return true;
  } catch {
    return false;
  }
}

function fieldMatches(field, value) {
  return field.any || field.values.has(value);
}

export function cronMatches(expression, wall) {
  if (
    !fieldMatches(expression.minute, wall.minute) ||
    !fieldMatches(expression.hour, wall.hour) ||
    !fieldMatches(expression.month, wall.month)
  ) {
    return false;
  }
  const dayMatches = fieldMatches(expression.day, wall.day);
  const weekdayMatches = fieldMatches(expression.weekday, wall.weekday);
  if (expression.day.any && expression.weekday.any) {
    return true;
  }
  if (expression.day.any) {
    return weekdayMatches;
  }
  if (expression.weekday.any) {
    return dayMatches;
  }
  return dayMatches || weekdayMatches;
}
