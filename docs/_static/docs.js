// Enhance document navigation while preserving the static HTML without JavaScript.
document.addEventListener('DOMContentLoaded', function () {
  var article = document.querySelector('main article');
  if (!article) return;

  article.querySelectorAll('table').forEach(function (table) {
    var frame = document.createElement('div');
    frame.className = 'table-scroll';
    frame.tabIndex = 0;
    frame.setAttribute('role', 'region');
    frame.setAttribute('aria-label', 'Reference table, scroll horizontally for more columns');
    var labels = Array.from(table.querySelectorAll('thead th')).map(function (cell) { return cell.textContent.trim(); });
    if (labels[0] === 'Parameter') {
      table.classList.add('parameter-table');
      table.setAttribute('role', 'table');
      table.querySelectorAll('tr').forEach(function (row) { row.setAttribute('role', 'row'); });
      table.querySelectorAll('thead th').forEach(function (cell) { cell.setAttribute('role', 'columnheader'); });
      table.querySelectorAll('tbody tr').forEach(function (row) {
        Array.from(row.cells).forEach(function (cell, index) {
          cell.dataset.label = labels[index];
          cell.setAttribute('role', 'cell');
        });
      });
    }
    table.before(frame);
    frame.appendChild(table);
  });
  function addCopyButton(container, text, label) {
    var button = document.createElement('button');
    button.className = 'copy-button outline small';
    button.type = 'button';
    button.textContent = 'Copy';
    button.setAttribute('aria-label', label);
    button.setAttribute('aria-live', 'polite');
    button.addEventListener('click', async function () {
      try {
        await navigator.clipboard.writeText(text);
        button.textContent = 'Copied';
      } catch (error) {
        button.textContent = 'Copy';
        button.title = 'Text selected. Use your device’s Copy action.';
        var selection = window.getSelection();
        var range = document.createRange();
        range.selectNodeContents(container.querySelector('pre, p'));
        selection.removeAllRanges();
        selection.addRange(range);
      }
      setTimeout(function () { button.textContent = 'Copy'; }, 2000);
    });
    container.appendChild(button);
  }
  article.querySelectorAll('pre').forEach(function (block) {
    block.tabIndex = 0;
    block.setAttribute('aria-label', 'Code example, scroll horizontally for more content');
    var frame = document.createElement('div');
    frame.className = 'code-frame';
    block.before(frame);
    frame.appendChild(block);
    addCopyButton(frame, block.textContent, 'Copy code example');
  });

  article.querySelectorAll('blockquote').forEach(function (quote) {
    addCopyButton(quote, quote.textContent.trim(), 'Copy example prompt');
  });

  var drawer = document.querySelector('aside[data-sidebar]');
  var toggle = document.querySelector('[data-sidebar-toggle]');
  var backdrop = document.createElement('button');
  backdrop.className = 'sidebar-backdrop';
  backdrop.tabIndex = -1;
  backdrop.setAttribute('aria-label', 'Close navigation');
  backdrop.addEventListener('click', function () {
    document.body.removeAttribute('data-sidebar-open');
    toggle.focus();
  });
  document.body.appendChild(backdrop);
  var mobile = matchMedia('(max-width: 768px)');
  function syncDrawer() {
    var open = document.body.hasAttribute('data-sidebar-open');
    drawer.inert = mobile.matches && !open;
    document.querySelector('main').inert = mobile.matches && open;
    toggle.setAttribute('aria-expanded', String(mobile.matches && open));
  }
  new MutationObserver(syncDrawer).observe(document.body, { attributes: true, attributeFilter: ['data-sidebar-open'] });
  mobile.addEventListener('change', syncDrawer);
  document.addEventListener('keydown', function (event) {
    if (event.key === 'Escape' && mobile.matches && document.body.hasAttribute('data-sidebar-open')) {
      document.body.removeAttribute('data-sidebar-open');
      toggle.focus();
    }
  });
  syncDrawer();

  var headings = Array.from(article.querySelectorAll('h2[id]'));
  if (headings.length < 3) return;
  var contents = document.createElement('details');
  contents.className = 'docs-contents';
  var summary = document.createElement('summary');
  summary.textContent = 'On this page';
  contents.appendChild(summary);
  var nav = document.createElement('nav');
  nav.setAttribute('aria-label', 'On this page');
  var list = document.createElement('ul');
  headings.forEach(function (heading) {
    var item = document.createElement('li');
    var link = document.createElement('a');
    link.href = '#' + heading.id;
    link.textContent = heading.textContent;
    item.appendChild(link);
    list.appendChild(item);
  });
  nav.appendChild(list);
  contents.appendChild(nav);
  var intro = article.querySelector('h1');
  if (intro) intro.after(contents);
});
