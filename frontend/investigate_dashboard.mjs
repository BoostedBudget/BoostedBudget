import { chromium } from '@playwright/test';
import fs from 'fs';

const consoleMessages = [];
const networkErrors = [];

const browser = await chromium.launch({ headless: true });
const context = await browser.newContext({ ignoreHTTPSErrors: true });
const page = await context.newPage();

// Capture console messages
page.on('console', msg => {
    consoleMessages.push({
        type: msg.type(),
        text: msg.text(),
        location: msg.location()
    });
});

// Capture network failures
page.on('requestfailed', request => {
    networkErrors.push({
        url: request.url(),
        method: request.method(),
        failure: request.failure()?.errorText
    });
});

console.log("Navigating to dashboard...");
try {
    await page.goto('https://qa.example.ts.net:3443/dashboard', {
        waitUntil: 'networkidle',
        timeout: 30000
    });
} catch (e) {
    console.log("Navigation error:", e.message);
}

await page.waitForTimeout(3000);
await page.screenshot({ path: '/tmp/dashboard_initial.png', fullPage: true });
console.log("Screenshot saved to /tmp/dashboard_initial.png");

// Find all buttons
console.log("\nAll buttons on page:");
const buttons = await page.locator('button').all();
for (let i = 0; i < Math.min(buttons.length, 20); i++) {
    try {
        const text = await buttons[i].textContent();
        const shortText = text ? text.slice(0, 50) : '(no text)';
        console.log("  " + i + ": '" + shortText + "'");
    } catch (e) {
        console.log("  " + i + ": error");
    }
}

// Look for add widget functionality
console.log("\nLooking for Add Widget...");
const addWidgetBtn = page.locator('button:has-text("Add Widget"), button:has-text("Add"), [aria-label*="Add"]');
const addCount = await addWidgetBtn.count();
console.log("Found " + addCount + " potential add buttons");

if (addCount > 0) {
    const first = addWidgetBtn.first();
    if (await first.isVisible()) {
        console.log("Clicking Add Widget button...");
        await first.click();
        await page.waitForTimeout(1000);
        await page.screenshot({ path: '/tmp/after_add_click.png', fullPage: true });
    }
}

// Look for Net Worth in catalog or on page
console.log("\nLooking for Net Worth...");
const netWorthElements = await page.locator('text=Net Worth').all();
console.log("Found " + netWorthElements.length + " Net Worth elements");

for (let i = 0; i < netWorthElements.length; i++) {
    try {
        const visible = await netWorthElements[i].isVisible();
        const text = await netWorthElements[i].textContent();
        const shortText = text ? text.slice(0, 50) : '';
        console.log("  " + i + ": visible=" + visible + ", text='" + shortText + "'");

        if (visible && text && text.includes('Net Worth')) {
            console.log("  Clicking element " + i + "...");
            await netWorthElements[i].click();
            await page.waitForTimeout(2000);
            await page.screenshot({ path: '/tmp/after_net_worth_click.png', fullPage: true });
            break;
        }
    } catch (e) {
        console.log("  " + i + ": error - " + e.message);
    }
}

await page.waitForTimeout(2000);
await page.screenshot({ path: '/tmp/dashboard_final.png', fullPage: true });

// Save HTML
const html = await page.content();
fs.writeFileSync('/tmp/dashboard_html.html', html);
console.log("\nPage HTML saved to /tmp/dashboard_html.html");

await browser.close();

// Print results
console.log("\n==================================================");
console.log("CONSOLE ERRORS:");
console.log("==================================================");
const errors = consoleMessages.filter(m => m.type === 'error' || m.type === 'warning');
if (errors.length) {
    errors.forEach(e => {
        const msgType = e.type.toUpperCase();
        const msgText = e.text.slice(0, 300);
        console.log("[" + msgType + "] " + msgText);
    });
} else {
    console.log("No console errors found");
}

console.log("\n==================================================");
console.log("NETWORK FAILURES:");
console.log("==================================================");
if (networkErrors.length) {
    networkErrors.forEach(e => console.log(e.method + " " + e.url + ": " + e.failure));
} else {
    console.log("No network failures");
}

console.log("\n==================================================");
console.log("ALL CONSOLE MESSAGES:");
console.log("==================================================");
consoleMessages.forEach(m => {
    const msgText = m.text.slice(0, 200);
    console.log("[" + m.type + "] " + msgText);
});
