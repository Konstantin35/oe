/* jshint node: true */

module.exports = function(environment) {
  var ExplorerBase, ApiUrl
  var coin = 'eth'
  if (coin === 'eth') {
    ExplorerBase = 'https://etherscan.io'
    ApiUrl = 'http://shengupiao.com:8090/'
  }
  if (coin === 'etc') {
    ExplorerBase = 'http://gastracker.io'
    ApiUrl = 'http://shengupiao.com:8091/'
  }
  var ENV = {
    modulePrefix: 'open-ethereum-pool',
    environment: environment,
    rootURL: '/',
    locationType: 'hash',
    EmberENV: {
      FEATURES: {
        // Here you can enable experimental features on an ember canary build
        // e.g. 'with-controller': true
      }
    },

    APP: {
      // API host and port
      ApiUrl: ApiUrl,

      minimalUI: true,
      partnerAddress: 'cn.f6pool.com:8099',

      coin: coin,
      ExplorerBase: ExplorerBase,

      // HTTP mining endpoint
      HttpHost: 'http://shengupiao.com',
      HttpPort: 8888,

      // Stratum mining endpoint
      StratumHost: 'shengupiao.com',
      StratumPort: 8008,

      // Fee and payout details
      PoolFee: '1%',
      PayoutThreshold: '0.5 ETH',

      // For network hashrate (change for your favourite fork)
      BlockTime: 14.4
    }
  };

  if (environment === 'development') {
    /* Override ApiUrl just for development, while you are customizing
      frontend markup and css theme on your workstation.
    */
    // ENV.APP.ApiUrl = 'http://localhost:8080/'
    // ENV.APP.ApiUrl = 'http://123.56.89.3:8080/'
    // ENV.APP.LOG_RESOLVER = true;
    // ENV.APP.LOG_ACTIVE_GENERATION = true;
    // ENV.APP.LOG_TRANSITIONS = true;
    // ENV.APP.LOG_TRANSITIONS_INTERNAL = true;
    // ENV.APP.LOG_VIEW_LOOKUPS = true;
  }

  if (environment === 'test') {
    // Testem prefers this...
    ENV.locationType = 'none';

    // keep test console output quieter
    ENV.APP.LOG_ACTIVE_GENERATION = false;
    ENV.APP.LOG_VIEW_LOOKUPS = false;

    ENV.APP.rootElement = '#ember-testing';
  }

  if (environment === 'production') {

  }

  return ENV;
};
