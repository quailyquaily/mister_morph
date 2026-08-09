function placeSteeredAgentsAfterUsers(items) {
  const ordered = Array.isArray(items) ? [...items] : [];
  const steers = ordered
    .filter((item) => String(item?.role || "") === "user")
    .map((item) => ({
      userID: String(item?.id || "").trim(),
      targetTaskID: String(item?.steerTargetTaskID || "").trim(),
    }))
    .filter((steer) => steer.userID && steer.targetTaskID);

  for (const steer of steers) {
    const agentIndex = ordered.findIndex(
      (item) =>
        String(item?.role || "") === "agent" &&
        String(item?.taskId || "").trim() === steer.targetTaskID
    );
    if (agentIndex < 0) {
      continue;
    }
    const [agentItem] = ordered.splice(agentIndex, 1);
    const userIndex = ordered.findIndex((item) => String(item?.id || "").trim() === steer.userID);
    if (userIndex < 0) {
      ordered.splice(Math.min(agentIndex, ordered.length), 0, agentItem);
      continue;
    }
    ordered.splice(userIndex + 1, 0, agentItem);
  }
  return ordered;
}

export { placeSteeredAgentsAfterUsers };
