function makeSortableId() {
  const ts = new Date()
    .toISOString()
    .replace(/[-:]/g, "")
    .replace(/\.\d{3}Z$/, "Z");
  const rand = crypto.randomUUID().replace(/-/g, "");
  return `${ts}_${rand}`;
}

function safePathPart(value) {
  return String(value || "unknown")
    .toLowerCase()
    .replace(/[^a-z0-9_.@+-]/g, "_");
}

export default {
  async email(message, env, ctx) {
    if (!env.MAIL_BUCKET || typeof env.MAIL_BUCKET.put !== "function") {
      throw new Error("MAIL_BUCKET is not an R2 bucket binding");
    }

    const now = new Date();
    const id = makeSortableId();
    const mailFrom = message.from || "";
    const rcptTo = message.to || "";
    const safeRcpt = safePathPart(rcptTo);
    const emailKey = `email/${safeRcpt}/${id}.eml`;

    /*
     * R2 requires a known content length for streams.
     * message.raw is a ReadableStream without a known length, so buffer it first.
     */
    const rawEmail = await new Response(message.raw).arrayBuffer();

    await env.MAIL_BUCKET.put(emailKey, rawEmail, {
      httpMetadata: {
        contentType: "message/rfc822",
      },
      customMetadata: {
        id,
        mailFrom,
        rcptTo,
        receivedAt: now.toISOString(),
        rawSize: String(message.rawSize || rawEmail.byteLength),
        messageId: message.headers.get("Message-ID") || "",
        subject: message.headers.get("Subject") || "",
      },
    });

    console.log("Stored inbound email", {
      id,
      mailFrom,
      rcptTo,
      rawSize: message.rawSize || rawEmail.byteLength,
      emailKey,
    });
  },
};
