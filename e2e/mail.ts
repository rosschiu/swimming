import { expect, type APIRequestContext } from '@playwright/test';

const mailURL = process.env.MAILPIT_URL || 'http://127.0.0.1:18025';

// Reads only the dedicated local SMTP capture, never a user's mailbox.
export async function emailLink(request: APIRequestContext, recipient: string, path: string, excludedID?: string) {
  let messageID = '';
  await expect.poll(async () => {
    const response = await request.get(`${mailURL}/api/v1/messages`);
    expect(response.ok()).toBe(true);
    const inbox = await response.json();
    const match = inbox.messages.find((message: { ID: string; To: { Address: string }[] }) => message.ID !== excludedID && message.To.some(to => to.Address === recipient));
    messageID = match?.ID || '';
    return messageID;
  }).not.toBe('');
  const response = await request.get(`${mailURL}/api/v1/message/${messageID}`);
  expect(response.ok()).toBe(true);
  const message = await response.json();
  const link = String(message.Text).match(/https?:\/\/[^\s<>]+/g)?.find(value => value.includes(path));
  expect(link, 'Expected account link in actual SMTP message').toBeTruthy();
  return { link: link!, messageID };
}
