module.exports = {
    plugins: [
      require('postcss-prefix-selector')({
        // Keep the prefix option empty and apply prefixes conditionally in transform
        prefix: '',
        transform: function (prefix, selector, prefixedSelector, filePath) {
          // Apply .dashboard prefix to dashboard CSS files
          if (filePath && filePath.indexOf('/assets/css/dashboard/') !== -1) {
            // Return selectors that already have the prefix as-is
            if (selector.startsWith('.dashboard')) return selector;
            return '.dashboard ' + selector;
          }
          // Return other selectors unchanged
          return selector;
        }
      })
    ]
  };
