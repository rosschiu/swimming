import { expect, test, type Page } from '@playwright/test';

async function checkDestination(page: Page) {
  const main = page.getByRole('main');
  await expect(main).toHaveCount(1);
  await expect(main).toBeFocused();
  await expect(main).toBeInViewport();
  expect(await main.evaluate(element => getComputedStyle(element).outlineStyle)).not.toBe('none');
  await page.keyboard.press('Tab');
  await expect(main.locator(':focus')).toHaveCount(1);
  await expect(page.getByRole('navigation').locator(':focus')).toHaveCount(0);
}

for (const language of ['en', 'zh']) {
  for (const width of [375, 1280]) {
    test(language + ' keyboard navigation bypasses the sidebar at ' + width + 'px', async ({ page }) => {
      await page.setViewportSize({ width, height: 800 });
      await page.addInitScript(lang => localStorage.setItem('language', lang), language);
      await page.goto('/students');
      const zh = language === 'zh';
      const skip = page.getByRole('link', { name: zh ? '跳至內容' : 'Skip to content' });
      async function activateSkip() {
        await skip.focus();
        await page.keyboard.press('Enter');
        await checkDestination(page);
      }
      const email = page.getByLabel(zh ? '電郵' : 'Email', { exact: true });
      await expect(email).toBeVisible();
      await activateSkip();
      await page.getByRole('button', { name: zh ? '忘記密碼？' : 'Forgot password?' }).click();
      await expect(page.getByRole('heading', { name: zh ? '重設密碼' : 'Reset your password' })).toBeVisible();
      await activateSkip();
      await expect(email).toBeFocused();
      await page.getByRole('button', { name: zh ? '返回登入' : 'Back to sign in' }).click();
      await email.fill('admin@swim.test');
      await page.getByLabel(zh ? '密碼' : 'Password', { exact: true }).fill('Swim-test-2026!');
      await page.getByRole('button', { name: zh ? '登入' : 'Sign in', exact: true }).click();
      const nav = page.getByRole('navigation');
      await expect(nav).toBeVisible();
      await page.reload();
      await expect(nav).toBeVisible();
      await page.keyboard.press('Tab');
      await expect(skip).toBeFocused();
      await page.keyboard.press('Enter');
      await checkDestination(page);
      const schedule = nav.getByRole('link', { name: zh ? '時間表' : 'Schedule' });
      await schedule.focus();
      await page.keyboard.press('Enter');
      await expect(page).toHaveURL(/\/schedule$/);
      await checkDestination(page);
      await page.goBack();
      await expect(page).toHaveURL(/\/students(?:#main)?$/);
      await checkDestination(page);
    });
  }
}
