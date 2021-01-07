import { StringParam, withQueryParams } from 'use-query-params';
import { handlePageVisibility, withPageVisibility } from './util/PageVisibility.jsx';

import Button from '@material-ui/core/Button';
import Card from '@material-ui/core/Card';
import CardContent from '@material-ui/core/CardContent';
import ConfigureProfilesMsg from './ConfigureProfilesMsg.jsx';
import Divider from '@material-ui/core/Divider';
import ErrorBanner from './ErrorBanner.jsx';
import FormControl from '@material-ui/core/FormControl';
import FormHelperText from '@material-ui/core/FormHelperText';
import Grid from '@material-ui/core/Grid';
import InputLabel from '@material-ui/core/InputLabel';
import MenuItem from '@material-ui/core/MenuItem';
import PropTypes from 'prop-types';
import QueryToCliCmd from './QueryToCliCmd.jsx';
import React from 'react';
import Select from '@material-ui/core/Select';
import TopRoutesModule from './TopRoutesModule.jsx';
import { Trans } from '@lingui/macro';
import Typography from '@material-ui/core/Typography';
import _get from 'lodash/get';
import _isEmpty from 'lodash/isEmpty';
import _merge from 'lodash/merge';
import _pick from 'lodash/pick';
import _uniq from 'lodash/uniq';
import { groupResourcesByNs } from './util/MetricUtils.jsx';
import { tapResourceTypes } from './util/TapUtils.jsx';
import { withContext } from './util/AppContext.jsx';
import { withStyles } from '@material-ui/core/styles';

const topRoutesQueryProps = {
  resource_name: PropTypes.string,
  resource_type: PropTypes.string,
  namespace: PropTypes.string,
  to_name: PropTypes.string,
  to_type: PropTypes.string,
  to_namespace: PropTypes.string,
};
const topRoutesQueryPropType = PropTypes.shape(topRoutesQueryProps);

const topRoutesQueryConfig = {};
Object.keys(topRoutesQueryProps).forEach(value => {
  topRoutesQueryConfig[value] = StringParam;
});

const toResourceName = (query, typeKey, nameKey) => {
  return `${query[typeKey] || ''}${!query[nameKey] ? '' : '/'}${query[nameKey] || ''}`;
};

const styles = theme => ({
  root: {
    marginTop: theme.spacing(3),
    marginBottom: theme.spacing(1),
  },
  formControl: {
    minWidth: 200,
  },
});

class TopRoutes extends React.Component {
  constructor(props) {
    super(props);
    this.api = props.api;

    const query = _merge({
      resource_name: '',
      resource_type: '',
      namespace: '',
      to_name: '',
      to_type: '',
      to_namespace: '',
    }, _pick(props.query, Object.keys(topRoutesQueryProps)));

    this.state = {
      query,
      error: null,
      services: [],
      namespaces: ['default'],
      resourcesByNs: {},
      pollingInterval: 5000,
      pendingRequests: false,
      requestInProgress: false,
    };
  }

  componentDidMount() {
    this._isMounted = true; // https://reactjs.org/blog/2015/12/16/ismounted-antipattern.html
    this.startServerPolling();
  }

  componentDidUpdate(prevProps) {
    const { isPageVisible } = this.props;
    handlePageVisibility({
      prevVisibilityState: prevProps.isPageVisible,
      currentVisibilityState: isPageVisible,
      onVisible: () => this.startServerPolling(),
      onHidden: () => this.stopServerPolling(),
    });
  }

  componentWillUnmount() {
    this._isMounted = false;
    this.stopServerPolling();
  }

  loadFromServer = () => {
    const { pendingRequests } = this.state;

    if (pendingRequests) {
      return; // don't make more requests if the ones we sent haven't completed
    }
    this.setState({ pendingRequests: true });

    const allMetricsUrl = this.api.urlsForResourceNoStats('all');
    this.api.setCurrentRequests([
      this.api.fetchServices(),
      this.api.fetchMetrics(allMetricsUrl),
    ]);

    this.serverPromise = Promise.all(this.api.getCurrentPromises())
      .then(([svcList, allMetrics]) => {
        const services = _get(svcList, 'services', []);
        const namespaces = _uniq(services.map(s => s.namespace));
        const { resourcesByNs } = groupResourcesByNs(allMetrics);

        this.setState({
          services,
          namespaces,
          resourcesByNs,
          pendingRequests: false,
          error: null,
        });
      })
      .catch(this.handleApiError);
  }

  handleApiError = e => {
    if (e.isCanceled) {
      return;
    }

    this.setState({
      pendingRequests: false,
      error: e,
    });
  }

  startServerPolling = () => {
    const { pollingInterval } = this.state;
    this.loadFromServer();
    this.timerId = window.setInterval(this.loadFromServer, pollingInterval);
  }

  stopServerPolling = () => {
    window.clearInterval(this.timerId);
    this.api.cancelCurrentRequests();
    this.setState({ pendingRequests: false });
  }

  handleBtnClick = inProgress => () => {
    this.setState({
      requestInProgress: inProgress,
    });
  }

  // Each time state.query is updated, this method calls setQuery provided
  // by useQueryParams HOC to partially update url query params that have
  // changed
  handleUrlUpdate = query => {
    const { setQuery } = this.props;
    setQuery({ ...query });
  }

  handleNamespaceSelect = nsKey => e => {
    const { query } = this.state;
    const newQuery = query;
    const formVal = _get(e, 'target.value');
    newQuery[nsKey] = formVal;
    this.handleUrlUpdate(newQuery);
    this.setState({ query: newQuery });
  };

  handleResourceSelect = (nameKey, typeKey) => e => {
    const { query } = this.state;
    const resource = _get(e, 'target.value');
    const [resourceType, resourceName] = resource.split('/');

    query[nameKey] = resourceName || '';
    query[typeKey] = resourceType;

    this.handleUrlUpdate(query);
    this.setState({ query });
  }

  renderRoutesQueryForm = () => {
    const { query, requestInProgress } = this.state;
    const { classes } = this.props;

    return (
      <CardContent>
        <Grid container direction="column" spacing={2}>
          <Grid item container spacing={4} alignItems="center" justify="flex-start">
            <Grid item>
              { this.renderNamespaceDropdown(<Trans>formNamespace</Trans>, 'namespace', <Trans>formNamespaceHelpText</Trans>) }
            </Grid>

            <Grid item>
              { this.renderResourceDropdown(<Trans>formResource</Trans>, 'resource_name', 'resource_type', <Trans>formResourceHelpText</Trans>) }
            </Grid>

            <Grid item>
              <Button
                color="primary"
                variant="outlined"
                disabled={requestInProgress || !query.namespace || !query.resource_type}
                onClick={this.handleBtnClick(true)}>
                <Trans>buttonStart</Trans>
              </Button>
            </Grid>

            <Grid item>
              <Button
                color="default"
                variant="outlined"
                disabled={!requestInProgress}
                onClick={this.handleBtnClick(false)}>
                <Trans>buttonStop</Trans>
              </Button>
            </Grid>
          </Grid>

          <Grid item container spacing={4} alignItems="center" justify="flex-start">
            <Grid item>
              { this.renderNamespaceDropdown(<Trans>formToNamespace</Trans>, 'to_namespace', <Trans>formToNamespaceHelpText</Trans>) }
            </Grid>

            <Grid item>
              { this.renderResourceDropdown(<Trans>formToResource</Trans>, 'to_name', 'to_type', <Trans>formToResourceHelpText</Trans>) }
            </Grid>
          </Grid>
        </Grid>
        <Divider light className={classes.root} />
        <Typography variant="caption"><Trans>createNewProfileMsg</Trans> <ConfigureProfilesMsg showAsIcon /></Typography>
      </CardContent>
    );
  }

  renderNamespaceDropdown = (title, key, helperText) => {
    const { query, namespaces } = this.state;
    const { classes } = this.props;

    return (
      <FormControl className={classes.formControl}>
        <InputLabel htmlFor={`${key}-dropdown`}>{title}</InputLabel>
        <Select
          value={namespaces.includes(query[key]) ? query[key] : ''}
          onChange={this.handleNamespaceSelect(key)}
          inputProps={{
            name: key,
            id: `${key}-dropdown`,
          }}
          name={key}>
          {
            namespaces.sort().map(ns => {
              return <MenuItem key={`namespace-${ns}`} value={ns}>{ns}</MenuItem>;
            })
          }
        </Select>
        <FormHelperText>{helperText}</FormHelperText>
      </FormControl>
    );
  }

  renderResourceDropdown = (title, nameKey, typeKey, helperText) => {
    const { query, services, resourcesByNs } = this.state;
    const { classes } = this.props;

    let nsFilterKey = 'namespace';
    if (typeKey === 'to_type') {
      nsFilterKey = 'to_namespace';
    }

    const servicesWithPrefix = services
      .filter(s => s[nsFilterKey] === query[nsFilterKey])
      .map(svc => `service/${svc.name}`);
    const otherResources = resourcesByNs[query[nsFilterKey]] || [];

    let dropdownOptions = servicesWithPrefix
      .concat(otherResources)
      .concat(tapResourceTypes)
      .sort();

    const dropdownVal = toResourceName(query, typeKey, nameKey);

    if (_isEmpty(dropdownOptions) && !_isEmpty(dropdownVal)) {
      dropdownOptions = [dropdownVal]; // populate from url if autocomplete hasn't loaded
    }

    return (
      <FormControl className={classes.formControl}>
        <InputLabel htmlFor={`${nameKey}-dropdown`}>{title}</InputLabel>
        <Select
          value={dropdownOptions.includes(dropdownVal) ? dropdownVal : ''}
          onChange={this.handleResourceSelect(nameKey, typeKey)}
          disabled={_isEmpty(query.namespace)}
          inputProps={{
            name: nameKey,
            id: `${nameKey}-dropdown`,
          }}
          name={nameKey}>
          {
            dropdownOptions.map(resource => <MenuItem key={resource} value={resource}>{resource}</MenuItem>)
          }
        </Select>
        <FormHelperText>{helperText}</FormHelperText>
      </FormControl>
    );
  }

  render() {
    const { query, requestInProgress, error } = this.state;
    const emptyQuery = _isEmpty(query.resource_type);
    const cliQueryToDisplay = _merge({}, query, { toResource: toResourceName(query, 'to_type', 'to_name'), toNamespace: query.to_namespace });

    return (
      <div>
        {
          !error ? null :
          <ErrorBanner message={error} onHideMessage={() => this.setState({ error: null })} />
        }
        <Card elevation={3}>
          { this.renderRoutesQueryForm() }
          {
            emptyQuery ? null :
            <QueryToCliCmd
              cmdName="routes"
              query={cliQueryToDisplay}
              resource={toResourceName(query, 'resource_type', 'resource_name')} />
            }
          { !requestInProgress || !this._isMounted ? null : <TopRoutesModule query={query} /> }
        </Card>
      </div>
    );
  }
}

TopRoutes.propTypes = {
  api: PropTypes.shape({
    PrefixedLink: PropTypes.func.isRequired,
  }).isRequired,
  isPageVisible: PropTypes.bool.isRequired,
  query: topRoutesQueryPropType.isRequired,
  setQuery: PropTypes.func.isRequired,
};

export default withPageVisibility(withQueryParams(topRoutesQueryConfig, (withContext(withStyles(styles, { withTheme: true })(TopRoutes)))));
