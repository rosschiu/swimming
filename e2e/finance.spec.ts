import { test, expect, type Page } from '@playwright/test';
import { expectContract } from './contract';
async function login(page:Page,role:string) {
 await page.goto('/');await page.getByLabel('Email',{exact:true}).fill(`${role}@swim.test`);await page.getByLabel('Password',{exact:true}).fill('Swim-test-2026!');await page.getByRole('button',{name:'Sign in',exact:true}).click();await expect(page.getByRole('button',{name:'Sign out'})).toBeVisible();
 const session=await(await page.request.get('/api/v1/auth/me')).json();return {Origin:'http://localhost:18573','X-CSRF-Token':session.csrfToken};
}
test('individual admin expiry and mock failure/retry/replay persist one credit grant and simulated receipt',async({page})=>{
 const suffix=Date.now();const name=`Finance package ${suffix}`;
 const headers=await login(page,'admin');
 // A fresh linked student keeps repeated and cumulative suite runs independent.
 const createdStudent=await page.request.post('/api/v1/students',{headers,data:{name:`Finance swimmer ${suffix}`,level:'Beginner'}});expect(createdStudent.status()).toBe(201);const student=await createdStudent.json();
 expect((await page.request.put(`/api/v1/students/${student.id}/relationships/parent`,{headers,data:{kind:'guardian'}})).status()).toBe(200);
 await page.getByRole('link',{name:'Packages and payments',exact:true}).click();
 await page.getByRole('button',{name:'Add package offer',exact:true}).click();await page.getByLabel('Package name',{exact:true}).fill(name);await page.getByLabel('Credits',{exact:true}).fill('10');await page.getByLabel('Price (HKD)',{exact:true}).fill('1234.56');
 await page.getByRole('button',{name:'Save package offer',exact:true}).click();await expect(page.getByRole('heading',{name,exact:true})).toBeVisible();
 const offers=await(await page.request.get('/api/v1/package-offers')).json();const offer=offers.items.find((x:{name:string})=>x.name===name);expectContract('PackageOffer',offer);expect(offer.priceMinor).toBe(123456);
 const settings=await(await page.request.get('/api/v1/settings/credits')).json();expectContract('CreditSettings',settings);expect(settings.policy.expiryEnabled).toBe(true);
 await page.getByRole('button',{name:'Sign out'}).click();await login(page,'parent');await page.getByRole('link',{name:'Packages and payments',exact:true}).click();await page.getByLabel('Choose child',{exact:true}).selectOption(student.id);await page.getByLabel('Package offer',{exact:true}).selectOption(offer.id);
 await page.getByRole('button',{name:'Create purchase',exact:true}).click();await expect(page.getByText('Awaiting administrator expiry',{exact:true})).toBeVisible();await expect(page.getByRole('button',{name:'Start mock payment'})).toHaveCount(0);
 const purchases=await(await page.request.get(`/api/v1/students/${student.id}/purchases`)).json();expect(purchases.items).toHaveLength(1);const purchase=purchases.items[0];expectContract('Purchase',purchase);expect(purchase.expiresOn).toBeNull();
 expect((await(await page.request.get(`/api/v1/students/${student.id}/credits`)).json()).items).toHaveLength(0);
 await page.getByRole('button',{name:'Sign out'}).click();await login(page,'admin');await page.getByRole('link',{name:'Packages and payments',exact:true}).click();
 const card=page.locator('article').filter({has:page.getByRole('heading',{name,exact:true})}).filter({hasText:purchase.id});await expect(card.getByText(new RegExp(student.name))).toBeVisible();await card.getByLabel('Expiry date (Hong Kong)',{exact:true}).fill('2035-12-31');await card.getByRole('button',{name:'Assign expiry',exact:true}).click();await expect(card).toHaveCount(0);
 await page.getByRole('button',{name:'Sign out'}).click();await login(page,'parent');await page.getByRole('link',{name:'Packages and payments',exact:true}).click();await page.getByLabel('Choose child',{exact:true}).selectOption(student.id);
 await page.getByRole('button',{name:'Start mock payment',exact:true}).click();await page.getByRole('button',{name:'Mark pending',exact:true}).click();await expect(page.getByText('No credit packages yet.')).toBeVisible();
 await page.getByRole('button',{name:'Mark failed',exact:true}).click();await expect(page.getByRole('button',{name:'Retry mock payment',exact:true})).toBeVisible();expect((await(await page.request.get(`/api/v1/students/${student.id}/credits`)).json()).items).toHaveLength(0);
 await page.getByRole('button',{name:'Retry mock payment',exact:true}).click();await page.getByRole('button',{name:'Mark succeeded',exact:true}).click();await expect(page.getByText('Paid (simulated)',{exact:true})).toBeVisible();await page.getByRole('button',{name:'Replay successful outcome',exact:true}).click();await expect(page.getByRole('button',{name:'View simulated receipt',exact:true})).toBeEnabled();
 await page.reload();await page.getByLabel('Choose child',{exact:true}).selectOption(student.id);await page.getByRole('button',{name:'View simulated receipt',exact:true}).click();await expect(page.getByRole('heading',{name:'Simulated receipt — no real payment',exact:true})).toBeVisible();
 const paid=await(await page.request.get(`/api/v1/purchases/${purchase.id}`)).json();expectContract('Purchase',paid);expect(paid.status).toBe('paid');expect(paid.attempts.map((a:{status:string})=>a.status)).toEqual(['failed','succeeded']);paid.attempts.forEach((a:unknown)=>expectContract('PaymentAttempt',a));
 const receipt=await(await page.request.get(`/api/v1/receipts/${paid.receiptId}`)).json();expectContract('Receipt',receipt);expect(receipt.simulated).toBe(true);expect(receipt.priceMinor).toBe(123456);
 const credits=await(await page.request.get(`/api/v1/students/${student.id}/credits`)).json();expect(credits.items).toHaveLength(1);expectContract('CreditPackage',credits.items[0]);expect(credits.items[0].available).toBe(10);expect(credits.items[0].movements).toHaveLength(1);expectContract('CreditMovement',credits.items[0].movements[0]);
 await page.getByRole('button',{name:'Sign out'}).click();await login(page,'admin');await page.getByRole('link',{name:'Packages and payments',exact:true}).click();await page.getByLabel('Choose student for credit history',{exact:true}).selectOption(student.id);await expect(page.getByRole('heading',{name:`Credit package ${credits.items[0].id}`,exact:true})).toBeVisible();
});
