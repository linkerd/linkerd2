module.exports = {
    plugins: {
      'postcss-import': {},
      'postcss-cssnext': {
        // support the last 2 versions of every browser with usage of 5% or greater
        browsers: ['last 2 versions', '> 5%'],
      },
    },
  };
