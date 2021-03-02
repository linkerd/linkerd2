import { StringParam, withQueryParams } from 'use-query-params';
import {
  defaultMaxRps,
  emptyTapQuery,
  httpMethods,
  tapQueryPropType,
  tapQueryProps,
  tapResourceTypes,
} from './util/TapUtils.jsx';
import Accordion from '@material-ui/core/Accordion';
import AccordionDetails from '@material-ui/core/AccordionDetails';
import AccordionSummary from '@material-ui/core/AccordionSummary';
import Button from '@material-ui/core/Button';
import Card from '@material-ui/core/Card';
import CardContent from '@material-ui/core/CardContent';
import ExpandMoreIcon from '@material-ui/icons/ExpandMore';
import FormControl from '@material-ui/core/FormControl';
import FormHelperText from '@material-ui/core/FormHelperText';
import Grid from '@material-ui/core/Grid';
import InputLabel from '@material-ui/core/InputLabel';
import MenuItem from '@material-ui/core/MenuItem';
import PropTypes from 'prop-types';
import QueryToCliCmd from './QueryToCliCmd.jsx';
import React from 'react';
import Select from '@material-ui/core/Select';
import TextField from '@material-ui/core/TextField';
import { Trans } from '@lingui/macro';
import Typography from '@material-ui/core/Typography';
import _flatten from 'lodash/flatten';
import _isEmpty from 'lodash/isEmpty';
import _isEqual from 'lodash/isEqual';
import _isNil from 'lodash/isNil';
import _map from 'lodash/map';
import _merge from 'lodash/merge';
import _noop from 'lodash/noop';
import _omit from 'lodash/omit';
import _pick from 'lodash/pick';
import _some from 'lodash/some';
import _uniq from 'lodash/uniq';
import _values from 'lodash/values';
import { withStyles } from '@material-ui/core/styles';

const getResourceList = (resourcesByNs, ns) => {
  return resourcesByNs[ns] || _uniq(_flatten(_values(resourcesByNs)));
};

const urlPropsQueryConfig = {};
Object.keys(tapQueryProps).forEach(value => {
  urlPropsQueryConfig[value] = StringParam;
});

const styles = theme => ({
  root: {
    display: 'flex',
    flexWrap: 'wrap',
  },
  formControlWrapper: {
    minWidth: 200,
  },
  formControl: {
    padding: theme.spacing(1),
    paddingLeft: 0,
    margin: 0,
    minWidth: 'inherit',
    maxWidth: '100%',
    width: 'auto',
  },
  selectEmpty: {
    'margin-top': '32px',
  },
  card: {
    maxWidth: '100%',
  },
  actions: {
    display: 'flex',
    'padding-left': '32px',
  },
  expand: {
    transform: 'rotate(0deg)',
    transition: theme.transitions.create('transform', {
      duration: theme.transitions.duration.shortest,
    }),
    marginLeft: 'auto',
    [theme.breakpoints.up('sm')]: {
      marginRight: -8,
    },
  },
  expandOpen: {
    transform: 'rotate(180deg)',
  },
  resetButton: {
    marginLeft: theme.spacing(1),
  },
});

class TapQueryForm extends React.Component {
  static getDerivedStateFromProps(props, state) {
    if (!_isEqual(props.resourcesByNs, state.resourcesByNs)) {
      const resourcesByNs = props.resourcesByNs;
      const authoritiesByNs = props.authoritiesByNs;
      const namespaces = Object.keys(resourcesByNs).sort();
      const resourceNames = getResourceList(resourcesByNs, state.query.namespace);
      const toResourceNames = getResourceList(resourcesByNs, state.query.toNamespace);
      const authorities = getResourceList(authoritiesByNs, state.query.namespace);

      return _merge(state, {
        resourcesByNs,
        authoritiesByNs,
        autocomplete: {
          namespace: namespaces,
          resource: resourceNames,
          toNamespace: namespaces,
          toResource: toResourceNames,
          authority: authorities,
        },
      });
    } else {
      return null;
    }
  }

  constructor(props) {
    super(props);

    const query = _merge({}, props.currentQuery, _pick(props.query, Object.keys(tapQueryProps)));
    props.updateQuery(query);

    const advancedFormExpanded = _some(
      _omit(query, ['namespace', 'resource']),
      v => !_isEmpty(v),
    );

    this.state = {
      query,
      advancedFormExpanded,
      authoritiesByNs: {},
      resourcesByNs: {},
      autocomplete: {
        namespace: [],
        resource: [],
        toNamespace: [],
        toResource: [],
        authority: [],
      },
    };
  }

  handleFormChange = (name, scopeResource) => {
    const { query, autocomplete, resourcesByNs, authoritiesByNs } = this.state;
    const { updateQuery } = this.props;

    const state = {
      query,
      autocomplete,
    };

    const shouldScopeAuthority = name === 'namespace';
    const newQueryValues = {};

    return event => {
      const formVal = event.target.value;
      state.query[name] = formVal;
      newQueryValues[name] = formVal;

      if (!_isNil(scopeResource)) {
        // scope the available typeahead resources to the selected namespace
        state.autocomplete[scopeResource] = resourcesByNs[formVal];
        state.query[scopeResource] = `namespace/${formVal}`;
        newQueryValues[scopeResource] = `namespace/${formVal}`;
      }

      if (shouldScopeAuthority) {
        state.autocomplete.authority = authoritiesByNs[formVal];
      }

      this.setState(state);
      updateQuery(state.query);
      this.handleUrlUpdate(newQueryValues);
    };
  }

  // Each time state.query is updated, this method calls setQuery provided
  // by useQueryParams HOC to partially update url query params that have
  // changed
  handleUrlUpdate = query => {
    const { setQuery } = this.props;
    setQuery({ ...query });
  }

  handleFormEvent = name => {
    const { query } = this.state;
    const { updateQuery } = this.props;

    const state = {
      query,
    };

    return event => {
      state.query[name] = event.target.value;
      this.handleUrlUpdate(state.query);
      this.setState(state);
      updateQuery(state.query);
    };
  }

  handleAdvancedFormExpandClick = () => {
    const { advancedFormExpanded } = this.state;
    this.setState({ advancedFormExpanded: !advancedFormExpanded });
  }

  autoCompleteData = name => {
    const { autocomplete, query } = this.state;
    return _uniq(
      autocomplete[name].filter(d => d.indexOf(query[name]) !== -1),
    ).sort();
  }

  resetTapForm = () => {
    const { updateQuery, handleTapClear } = this.props;

    this.setState({
      query: emptyTapQuery(),
    });

    this.handleUrlUpdate(emptyTapQuery());

    updateQuery(emptyTapQuery(), true);
    handleTapClear();
  }

  renderResourceSelect = (resourceKey, namespaceKey) => {
    const { autocomplete, query } = this.state;
    const { classes } = this.props;

    const selectedNs = query[namespaceKey];
    const nsEmpty = _isNil(selectedNs) || _isEmpty(selectedNs);

    const resourceOptions = tapResourceTypes.concat(
      autocomplete[resourceKey] || [],
      nsEmpty ? [] : [`namespace/${selectedNs}`],
    ).sort();

    return (
      <React.Fragment>
        <InputLabel htmlFor={resourceKey}>{resourceKey === 'resource' ? <Trans>formResource</Trans> : <Trans>formToResource</Trans>}</InputLabel>
        <Select
          value={!nsEmpty && resourceOptions.includes(query[resourceKey]) ? query[resourceKey] : ''}
          onChange={this.handleFormChange(resourceKey)}
          inputProps={{ name: resourceKey, id: resourceKey }}
          className={classes.selectEmpty}>
          {
            resourceOptions.map(resource => (
              <MenuItem key={`${namespaceKey}-${resourceKey}-${resource}`} value={resource}>{resource}</MenuItem>
            ))
          }
        </Select>
      </React.Fragment>
    );
  }

  renderNamespaceSelect = (title, namespaceKey, resourceKey) => {
    const { autocomplete, query } = this.state;
    const { classes } = this.props;

    return (
      <React.Fragment>
        <InputLabel htmlFor={namespaceKey}>{title}</InputLabel>
        <Select
          value={autocomplete[namespaceKey].includes(query[namespaceKey]) ? query[namespaceKey] : ''}
          onChange={this.handleFormChange(namespaceKey, resourceKey)}
          inputProps={{ name: namespaceKey, id: namespaceKey }}
          className={classes.selectEmpty}>
          {
            _map(autocomplete[namespaceKey], (n, i) => (
              <MenuItem key={`ns-dr-${i}`} value={n}>{n}</MenuItem>
            ))
          }
        </Select>
      </React.Fragment>
    );
  }

  renderTapButton = (tapInProgress, tapIsClosing) => {
    const { query } = this.state;
    const { handleTapStart, handleTapStop } = this.props;

    if (tapIsClosing) {
      return (
        <Button variant="outlined" color="primary" className="tap-ctrl tap-stop" disabled>
          <Trans>buttonStop</Trans>
        </Button>
      );
    } else if (tapInProgress) {
      return (
        <Button variant="outlined" color="primary" className="tap-ctrl tap-stop" onClick={handleTapStop}>
          <Trans>buttonStop</Trans>
        </Button>
      );
    } else {
      return (
        <Button
          color="primary"
          variant="outlined"
          className="tap-ctrl tap-start"
          disabled={!query.namespace || !query.resource}
          onClick={handleTapStart}>
          <Trans>buttonStart</Trans>
        </Button>
      );
    }
  }

  renderTextInput = (title, key, helperText) => {
    const { query } = this.state;
    const { classes } = this.props;

    return (
      <TextField
        id={key}
        label={title}
        className={classes.formControl}
        value={query[key]}
        onChange={this.handleFormEvent(key)}
        helperText={helperText}
        margin="normal" />
    );
  }

  renderAdvancedTapFormContent() {
    const { autocomplete, query } = this.state;
    const { classes } = this.props;

    return (
      <Grid container>

        <Grid container spacing={3}>
          <Grid item xs={6} md={3} className={classes.formControlWrapper}>
            <FormControl className={classes.formControl}>
              {this.renderNamespaceSelect(<Trans>formToNamespace</Trans>, 'toNamespace', 'toResource')}
            </FormControl>
          </Grid>
          <Grid item xs={6} md={3} className={classes.formControlWrapper}>
            <FormControl className={classes.formControl} disabled={_isEmpty(query.toNamespace)}>
              {this.renderResourceSelect('toResource', 'toNamespace')}
            </FormControl>
          </Grid>
        </Grid>

        <Grid container spacing={3}>
          <Grid item xs={6} md={3} classes={{ item: classes.formControlWrapper }}>
            <FormControl className={classes.formControl}>
              <InputLabel htmlFor="authority"><Trans>formAuthority</Trans></InputLabel>
              <Select
                value={query.authority}
                onChange={this.handleFormChange('authority')}
                inputProps={{ name: 'authority', id: 'authority' }}
                className={classes.selectEmpty}>
                {
                  _map(autocomplete.authority, (d, i) => (
                    <MenuItem key={`authority-${i}`} value={d}>{d}</MenuItem>
                  ))
                }
              </Select>
              <FormHelperText><Trans>formAuthorityHelpText</Trans></FormHelperText>
            </FormControl>
          </Grid>
          <Grid item xs={6} md={3} className={classes.formControlWrapper}>
            {this.renderTextInput(<Trans>formPath</Trans>, 'path', <Trans>formPathHelpText</Trans>)}
          </Grid>
        </Grid>

        <Grid container spacing={3}>
          <Grid item xs={6} md={3} className={classes.formControlWrapper}>
            {this.renderTextInput(<Trans>formScheme</Trans>, 'scheme', <Trans>formSchemeHelpText</Trans>)}
          </Grid>
          <Grid item xs={6} md={3} className={classes.formControlWrapper}>
            {this.renderTextInput(<Trans>formMaxRPS</Trans>, 'maxRps', <Trans>formMaxRPSHelpText {defaultMaxRps}</Trans>)}
          </Grid>
          <Grid item xs={6} md={3} className={classes.formControlWrapper}>
            <FormControl className={classes.formControl}>
              <InputLabel htmlFor="method"><Trans>formHTTPMethod</Trans></InputLabel>
              <Select
                value={query.method}
                onChange={this.handleFormChange('method')}
                inputProps={{ name: 'method', id: 'method' }}
                className={classes.selectEmpty}>
                {
                  _map(httpMethods, d => (
                    <MenuItem key={`method-${d}`} value={d}>{d}</MenuItem>
                  ))
                }
              </Select>
              <FormHelperText><Trans>formHTTPMethodHelpText</Trans></FormHelperText>
            </FormControl>
          </Grid>
        </Grid>

      </Grid>
    );
  }

  renderAdvancedTapForm() {
    const { advancedFormExpanded } = this.state;

    return (
      <Accordion expanded={advancedFormExpanded} onChange={this.handleAdvancedFormExpandClick} elevation={3}>
        <AccordionSummary expandIcon={<ExpandMoreIcon />}>
          <Typography variant="caption" gutterBottom>
            {advancedFormExpanded ? <Trans>formHideFilters</Trans> : <Trans>formShowFilters</Trans>}
          </Typography>
        </AccordionSummary>

        <AccordionDetails>
          {this.renderAdvancedTapFormContent()}
        </AccordionDetails>
      </Accordion>
    );
  }

  render() {
    const { query } = this.state;
    const { tapRequestInProgress, tapIsClosing, cmdName, enableAdvancedForm, classes } = this.props;

    return (
      <Card className={classes.card} elevation={3}>
        <CardContent>
          <Grid container spacing={3}>
            <Grid item xs={6} md="auto" className={classes.formControlWrapper}>
              <FormControl className={classes.formControl} fullWidth>
                {this.renderNamespaceSelect(<Trans>formNamespace</Trans>, 'namespace', 'resource')}
              </FormControl>
            </Grid>

            <Grid item xs={6} md="auto" className={classes.formControlWrapper}>
              <FormControl className={classes.formControl} disabled={_isEmpty(query.namespace)} fullWidth>
                {this.renderResourceSelect('resource', 'namespace')}
              </FormControl>
            </Grid>

            <Grid item>
              {this.renderTapButton(tapRequestInProgress, tapIsClosing)}
              <Button onClick={this.resetTapForm} disabled={tapRequestInProgress} className={classes.resetButton}>
                <Trans>buttonReset</Trans>
              </Button>
            </Grid>
          </Grid>
        </CardContent>

        <QueryToCliCmd cmdName={cmdName} query={query} resource={query.resource} />

        {!enableAdvancedForm ? null : this.renderAdvancedTapForm()}

      </Card>
    );
  }
}

TapQueryForm.propTypes = {
  authoritiesByNs: PropTypes.shape({}).isRequired,
  cmdName: PropTypes.string.isRequired,
  currentQuery: tapQueryPropType.isRequired,
  enableAdvancedForm: PropTypes.bool,
  handleTapClear: PropTypes.func,
  handleTapStart: PropTypes.func.isRequired,
  handleTapStop: PropTypes.func.isRequired,
  query: tapQueryPropType.isRequired,
  resourcesByNs: PropTypes.shape({}).isRequired,
  setQuery: PropTypes.func.isRequired,
  tapIsClosing: PropTypes.bool,
  tapRequestInProgress: PropTypes.bool.isRequired,
  updateQuery: PropTypes.func.isRequired,
};

TapQueryForm.defaultProps = {
  enableAdvancedForm: true,
  handleTapClear: _noop,
  tapIsClosing: false,
};

export default withQueryParams(urlPropsQueryConfig, withStyles(styles)(TapQueryForm));
