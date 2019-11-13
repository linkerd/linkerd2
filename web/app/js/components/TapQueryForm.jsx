import { StringParam, withQueryParams } from 'use-query-params';
import {
  defaultMaxRps,
  emptyTapQuery,
  httpMethods,
  tapQueryPropType,
  tapQueryProps,
  tapResourceTypes
} from './util/TapUtils.jsx';
import Button from '@material-ui/core/Button';
import Card from '@material-ui/core/Card';
import CardContent from '@material-ui/core/CardContent';
import ExpandMoreIcon from '@material-ui/icons/ExpandMore';
import ExpansionPanel from '@material-ui/core/ExpansionPanel';
import ExpansionPanelDetails from '@material-ui/core/ExpansionPanelDetails';
import ExpansionPanelSummary from '@material-ui/core/ExpansionPanelSummary';
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
import _startCase from 'lodash/startCase';
import _uniq from 'lodash/uniq';
import _values from 'lodash/values';
import { withStyles } from '@material-ui/core/styles';


const getResourceList = (resourcesByNs, ns) => {
  return resourcesByNs[ns] || _uniq(_flatten(_values(resourcesByNs)));
};

let urlPropsQueryConfig = {};
for (let value in tapQueryProps) {
  urlPropsQueryConfig[value] = StringParam;
}

const styles = theme => ({
  root: {
    display: 'flex',
    flexWrap: 'wrap',
  },
  formControlWrapper: {
    minWidth: 200,
  },
  formControl: {
    padding: theme.spacing.unit,
    paddingLeft: 0,
    margin: 0,
    minWidth: 'inherit',
    maxWidth: '100%',
    width: 'auto',
  },
  selectEmpty: {
    'margin-top': '32px'
  },
  card: {
    maxWidth: "100%",
  },
  actions: {
    display: 'flex',
    'padding-left': '32px'
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
    marginLeft: theme.spacing.unit,
  }
});


class TapQueryForm extends React.Component {
  static propTypes = {
    classes: PropTypes.shape({}).isRequired,
    cmdName: PropTypes.string.isRequired,
    currentQuery: tapQueryPropType.isRequired,
    enableAdvancedForm: PropTypes.bool,
    handleTapClear: PropTypes.func,
    handleTapStart: PropTypes.func.isRequired,
    handleTapStop: PropTypes.func.isRequired,
    query: tapQueryPropType.isRequired,
    setQuery: PropTypes.func.isRequired,
    tapIsClosing: PropTypes.bool,
    tapRequestInProgress: PropTypes.bool.isRequired,
    updateQuery: PropTypes.func.isRequired
  }

  static defaultProps = {
    enableAdvancedForm: true,
    handleTapClear: _noop,
    tapIsClosing: false
  }

  static getDerivedStateFromProps(props, state) {
    if (!_isEqual(props.resourcesByNs, state.resourcesByNs)) {
      let resourcesByNs = props.resourcesByNs;
      let authoritiesByNs = props.authoritiesByNs;
      let namespaces = Object.keys(resourcesByNs).sort();
      let resourceNames  = getResourceList(resourcesByNs, state.query.namespace);
      let toResourceNames = getResourceList(resourcesByNs, state.query.toNamespace);
      let authorities = getResourceList(authoritiesByNs, state.query.namespace);

      return _merge(state, {
        resourcesByNs,
        authoritiesByNs,
        autocomplete: {
          namespace: namespaces,
          resource: resourceNames,
          toNamespace: namespaces,
          toResource: toResourceNames,
          authority: authorities
        }
      });
    } else {
      return null;
    }
  }

  constructor(props) {
    super(props);

    const query = _merge({}, props.currentQuery, _pick(props.query, Object.keys(tapQueryProps)));
    props.updateQuery(query);

    let advancedFormExpanded = _some(
      _omit(query, ['namespace', 'resource']),
      v => !_isEmpty(v));

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
        authority: []
      },
    };
  }

  handleFormChange = (name, scopeResource) => {
    let state = {
      query: this.state.query,
      autocomplete: this.state.autocomplete
    };

    let shouldScopeAuthority = name === "namespace";
    let newQueryValues = {};

    return event => {
      let formVal = event.target.value;
      state.query[name] = formVal;
      newQueryValues[name] = formVal;

      if (!_isNil(scopeResource)) {
        // scope the available typeahead resources to the selected namespace
        state.autocomplete[scopeResource] = this.state.resourcesByNs[formVal];
        state.query[scopeResource] = `namespace/${formVal}`;
        newQueryValues[scopeResource] = `namespace/${formVal}`;
      }

      if (shouldScopeAuthority) {
        state.autocomplete.authority = this.state.authoritiesByNs[formVal];
      }

      this.setState(state);
      this.props.updateQuery(state.query);
      this.handleUrlUpdate(newQueryValues);
    };
  }

  // Each time state.query is updated, this method calls setQuery provided
  // by useQueryParams HOC to partially update url query params that have
  // changed
  handleUrlUpdate = query => {
    this.props.setQuery({ ...query });
  }

  handleFormEvent = name => {
    let state = {
      query: this.state.query
    };

    return event => {
      state.query[name] = event.target.value;
      this.handleUrlUpdate(state.query);
      this.setState(state);
      this.props.updateQuery(state.query);
    };
  }

  handleAdvancedFormExpandClick = () => {
    this.setState(state => ({advancedFormExpanded: !state.advancedFormExpanded}));
  }

  autoCompleteData = name => {
    return _uniq(
      this.state.autocomplete[name].filter(d => d.indexOf(this.state.query[name]) !== -1)
    ).sort();
  }

  resetTapForm = () => {
    this.setState({
      query: emptyTapQuery()
    });

    this.handleUrlUpdate(emptyTapQuery());

    this.props.updateQuery(emptyTapQuery(), true);
    this.props.handleTapClear();
  }

  renderResourceSelect = (resourceKey, namespaceKey) => {
    let selectedNs = this.state.query[namespaceKey];
    let nsEmpty = _isNil(selectedNs) || _isEmpty(selectedNs);
    let { classes } = this.props;

    let resourceOptions = tapResourceTypes.concat(
      this.state.autocomplete[resourceKey] || [],
      nsEmpty ? [] : [`namespace/${selectedNs}`]
    ).sort();

    return (
      <React.Fragment>
        <InputLabel htmlFor={resourceKey}>{_startCase(resourceKey)}</InputLabel>
        <Select
          value={!nsEmpty && resourceOptions.includes(this.state.query[resourceKey]) ? this.state.query[resourceKey] : ""}
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
    let { classes } = this.props;
    return (
      <React.Fragment>
        <InputLabel htmlFor={namespaceKey}>{title}</InputLabel>
        <Select
          value={this.state.autocomplete[namespaceKey].includes(this.state.query[namespaceKey]) ? this.state.query[namespaceKey] : ""}
          onChange={this.handleFormChange(namespaceKey, resourceKey)}
          inputProps={{ name: namespaceKey, id: namespaceKey }}
          className={classes.selectEmpty}>
          {
            _map(this.state.autocomplete[namespaceKey], (n, i) => (
              <MenuItem key={`ns-dr-${i}`} value={n}>{n}</MenuItem>
            ))
          }
        </Select>
      </React.Fragment>
    );
  }

  renderTapButton = (tapInProgress, tapIsClosing) => {
    if (tapIsClosing) {
      return (<Button variant="outlined" color="primary" className="tap-ctrl tap-stop" disabled={true}>Stop</Button>);
    } else if (tapInProgress) {
      return (<Button variant="outlined" color="primary" className="tap-ctrl tap-stop" onClick={this.props.handleTapStop}>Stop</Button>);
    } else {
      return (
        <Button
          color="primary"
          variant="outlined"
          className="tap-ctrl tap-start"
          disabled={!this.state.query.namespace || !this.state.query.resource}
          onClick={this.props.handleTapStart}>
          Start
        </Button>);
    }
  }

  renderTextInput = (title, key, helperText) => {
    let { classes } = this.props;
    return (
      <TextField
        id={key}
        label={title}
        className={classes.formControl}
        value={this.state.query[key]}
        onChange={this.handleFormEvent(key)}
        helperText={helperText}
        margin="normal" />
    );
  }

  renderAdvancedTapFormContent() {
    const { classes } = this.props;

    return (
      <Grid container>

        <Grid container spacing={3}>
          <Grid item xs={6} md={3} className={classes.formControlWrapper}>
            <FormControl className={classes.formControl}>
              {this.renderNamespaceSelect("To Namespace", "toNamespace", "toResource")}
            </FormControl>
          </Grid>
          <Grid item xs={6} md={3} className={classes.formControlWrapper}>
            <FormControl className={classes.formControl} disabled={_isEmpty(this.state.query.toNamespace)}>
              {this.renderResourceSelect("toResource", "toNamespace")}
            </FormControl>
          </Grid>
        </Grid>

        <Grid container spacing={3}>
          <Grid item xs={6} md={3} classes={{ item: classes.formControlWrapper }}>
            <FormControl className={classes.formControl}>
              <InputLabel htmlFor="authority">Authority</InputLabel>
              <Select
                value={this.state.query.authority}
                onChange={this.handleFormChange("authority")}
                inputProps={{ name: 'authority', id: 'authority' }}
                className={classes.selectEmpty}>
                {
                _map(this.state.autocomplete.authority, (d, i) => (
                  <MenuItem key={`authority-${i}`} value={d}>{d}</MenuItem>
                ))
              }
              </Select>
              <FormHelperText>Display requests with this :authority</FormHelperText>
            </FormControl>
          </Grid>
          <Grid item xs={6} md={3} className={classes.formControlWrapper}>
            { this.renderTextInput("Path", "path", "Display requests with paths that start with this prefix") }
          </Grid>
        </Grid>

        <Grid container spacing={3}>
          <Grid item xs={6} md={3} className={classes.formControlWrapper}>
            { this.renderTextInput("Scheme", "scheme", "Display requests with this scheme") }
          </Grid>
          <Grid item xs={6} md={3} className={classes.formControlWrapper}>
            { this.renderTextInput("Max RPS", "maxRps", `Maximum requests per second to tap. Default ${defaultMaxRps}`) }
          </Grid>
          <Grid item xs={6} md={3} className={classes.formControlWrapper}>
            <FormControl className={classes.formControl}>
              <InputLabel htmlFor="method">HTTP method</InputLabel>
              <Select
                value={this.state.query.method}
                onChange={this.handleFormChange("method")}
                inputProps={{ name: 'method', id: 'method' }}
                className={classes.selectEmpty}>
                {
                _map(httpMethods, d => (
                  <MenuItem key={`method-${d}`} value={d}>{d}</MenuItem>
                ))
              }
              </Select>
              <FormHelperText>Display requests with this HTTP method</FormHelperText>
            </FormControl>
          </Grid>
        </Grid>

      </Grid>
    );
  }

  renderAdvancedTapForm() {
    return (
      <ExpansionPanel expanded={this.state.advancedFormExpanded} onChange={this.handleAdvancedFormExpandClick} elevation={3}>
        <ExpansionPanelSummary expandIcon={<ExpandMoreIcon />}>
          <Typography variant="caption" gutterBottom>
            {this.state.advancedFormExpanded ? "Hide filters" : "Show more filters"}
          </Typography>
        </ExpansionPanelSummary>

        <ExpansionPanelDetails>
          {this.renderAdvancedTapFormContent()}
        </ExpansionPanelDetails>
      </ExpansionPanel>
    );
  }

  render() {
    const { classes } = this.props;

    return (
      <Card className={classes.card} elevation={3}>
        <CardContent>
          <Grid container spacing={3}>
            <Grid item xs={6} md="auto" className={classes.formControlWrapper}>
              <FormControl className={classes.formControl} fullWidth>
                {this.renderNamespaceSelect("Namespace", "namespace", "resource")}
              </FormControl>
            </Grid>

            <Grid item xs={6} md="auto" className={classes.formControlWrapper}>
              <FormControl className={classes.formControl} disabled={_isEmpty(this.state.query.namespace)} fullWidth>
                {this.renderResourceSelect("resource", "namespace")}
              </FormControl>
            </Grid>

            <Grid item>
              { this.renderTapButton(this.props.tapRequestInProgress, this.props.tapIsClosing) }
              <Button onClick={this.resetTapForm} disabled={this.props.tapRequestInProgress} className={classes.resetButton}>Reset</Button>
            </Grid>
          </Grid>
        </CardContent>

        <QueryToCliCmd cmdName={this.props.cmdName} query={this.state.query} resource={this.state.query.resource} />

        { !this.props.enableAdvancedForm ? null : this.renderAdvancedTapForm() }

      </Card>
    );
  }
}

export default withQueryParams(urlPropsQueryConfig, withStyles(styles)(TapQueryForm));
