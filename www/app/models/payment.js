import Ember from 'ember';
import config from '../config/environment';

var Payment = Ember.Object.extend({
  ExplorerBase: config.APP.ExplorerBase,
  addrBase: config.APP.coin === 'eth' ? 'address' : 'addr',
	formatAmount: Ember.computed('amount', function() {
		var value = parseInt(this.get('amount')) * 0.000000001;
		return value.toFixed(8);
	})
});

export default Payment;
